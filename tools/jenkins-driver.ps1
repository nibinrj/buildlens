#Requires -Version 7.4
Import-Module Microsoft.PowerShell.Utility -ErrorAction Stop
function Read-BuildLensEnv([string]$Path) {
    $values=@{}
    foreach($line in [IO.File]::ReadAllLines($Path)) {
        if($line -match '^\s*([A-Z][A-Z0-9_]*)=(.*)$') {
            $value=$Matches[2].Trim()
            if($value.Length -ge 2 -and (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'")))) { $value=$value.Substring(1,$value.Length-2) }
            $values[$Matches[1]]=$value
        }
    }
    return $values
}

function Invoke-JenkinsRequest([string]$Method,[uri]$Uri,[hashtable]$Headers,[hashtable]$Body=@{},[int]$RequestTimeout=20) {
    try {
        $options=@{Method=$Method;Uri=$Uri;Headers=$Headers;TimeoutSec=$RequestTimeout;MaximumRedirection=0;SkipHttpErrorCheck=$true;ErrorAction='Stop'}
        if($Method -eq 'POST') { $options.Body=$Body }
        if($script:driverSession){$options.WebSession=$script:driverSession}
        return Invoke-WebRequest @options
    } catch { throw "Jenkins $Method request failed; outcome may be uncertain. No automatic submission retry. URI: $($Uri.AbsolutePath)" }
}

function Convert-JenkinsJson($Response) {
    if([int]$Response.StatusCode -ne 200) { throw "Jenkins read returned HTTP $($Response.StatusCode)" }
    try { return $Response.Content | ConvertFrom-Json -ErrorAction Stop }
    catch { throw 'Jenkins returned invalid JSON' }
}

function Get-DriverNow { return [datetime]::UtcNow }
function Wait-DriverPoll([int]$Seconds) { Start-Sleep -Seconds $Seconds }

function Invoke-BuildDriver {
    [CmdletBinding()]
    param([int]$Count,[string]$Job,[hashtable]$Parameters,[int]$TimeoutSeconds,[int]$PollSeconds,[string]$EnvFile,[string]$EvidenceDirectory,[switch]$AllowMissingUpload,[switch]$UsePassword,[switch]$MeasureMemory)
    $ErrorActionPreference='Stop'
    if($Count -lt 1 -or $Count -gt 1000 -or $TimeoutSeconds -lt 1 -or $PollSeconds -lt 1) { throw 'Invalid driver limits' }
    $parts=$Job.Split('/')
    if(@($parts | Where-Object {$_ -eq '' -or $_ -in @('.','..') -or $_ -match '[\x00-\x1f\\?#]'}).Count) { throw 'Job must be a Jenkins folder/job path' }
    $settings=Read-BuildLensEnv $EnvFile
    if(-not $settings.JENKINS_ADMIN_USER){throw 'JENKINS_ADMIN_USER is required in the ignored environment file'}
    $secret=$settings.JENKINS_API_TOKEN
    if(-not $secret -and $UsePassword){$secret=$settings.JENKINS_ADMIN_PASSWORD}
    if(-not $secret){throw 'JENKINS_API_TOKEN is required; alternatively explicitly select -UsePassword for the existing local login'}
    $base=if($settings.JENKINS_URL){$settings.JENKINS_URL}else{"http://localhost:$(if($settings.JENKINS_HOST_PORT){$settings.JENKINS_HOST_PORT}else{'8080'})/"}
    $baseUri=[uri]($base.TrimEnd('/')+'/')
    if(-not $baseUri.IsAbsoluteUri -or $baseUri.UserInfo -or $baseUri.Query -or $baseUri.Fragment -or $baseUri.Scheme -notin @('http','https') -or ($baseUri.Scheme -eq 'http' -and -not $baseUri.IsLoopback)) { throw 'Jenkins URL must be HTTPS or loopback HTTP, without credentials/query/fragment' }
    $path=($parts | ForEach-Object {'job/'+[uri]::EscapeDataString($_)}) -join '/'
    $jobUri=[uri]::new($baseUri,$path+'/')
    $credentials=[Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($settings.JENKINS_ADMIN_USER+':'+$secret))
    $headers=@{Authorization='Basic '+$credentials}
    $script:driverSession=[Microsoft.PowerShell.Commands.WebRequestSession]::new()
    if(-not $settings.JENKINS_API_TOKEN){
        $crumb=Convert-JenkinsJson (Invoke-JenkinsRequest GET ([uri]::new($baseUri,'crumbIssuer/api/json')) $headers)
        if($crumb.crumbRequestField -notmatch '^[A-Za-z][A-Za-z0-9-]+$' -or -not $crumb.crumb -or $crumb.crumb.Length -gt 512){throw 'Invalid Jenkins CSRF crumb response'}
        $headers[$crumb.crumbRequestField]=$crumb.crumb
    }
    if(-not $EvidenceDirectory) { $EvidenceDirectory=Join-Path (Split-Path $PSScriptRoot -Parent) ('notes/experiments/drive-'+[datetime]::UtcNow.ToString('yyyyMMdd-HHmmss')) }
    $null=New-Item -ItemType Directory -Force -Path $EvidenceDirectory
    if(Test-Path (Join-Path $EvidenceDirectory 'run.json')) { throw 'Evidence directory already contains a run; use a new directory' }
    $record=[ordered]@{started_at=(Get-DriverNow).ToString('o');finished_at=$null;job=$Job;requested=$Count;parameters=$Parameters;builds=@();error=$null;verified_uploads=0}
    $recordPath=Join-Path $EvidenceDirectory 'run.json'
    function Save-DriverRecord {
        $record.verified_uploads=@($record.builds | Where-Object {$_.result -in @('SUCCESS','UNSTABLE') -and $_.buildlens_id -and -not $_.error}).Count
        [IO.File]::WriteAllText([IO.Path]::GetFullPath($recordPath+'.tmp'),($record | ConvertTo-Json -Depth 8))
        Move-Item -LiteralPath ($recordPath+'.tmp') -Destination $recordPath -Force
    }
    function Invoke-BoundedJenkinsRequest([string]$Method,[uri]$Uri,[hashtable]$Headers,[hashtable]$Body=@{}) {
        $remaining=($deadline-(Get-DriverNow)).TotalSeconds
        if($remaining -le 0) { throw 'Build deadline exceeded before Jenkins request' }
        return Invoke-JenkinsRequest $Method $Uri $Headers $Body ([int][Math]::Min(20,[Math]::Ceiling($remaining)))
    }
    Save-DriverRecord
    $monitor=$null
    try {
        if($MeasureMemory){
            $processInfo=[Diagnostics.ProcessStartInfo]::new((Get-Command pwsh -ErrorAction Stop).Source)
            $processInfo.UseShellExecute=$false
            $processInfo.CreateNoWindow=$true
            foreach($argument in @('-NoProfile','-File',(Join-Path $PSScriptRoot 'measure-memory.ps1'),'-RunFile',[IO.Path]::GetFullPath($recordPath),'-OutputFile',[IO.Path]::GetFullPath((Join-Path $EvidenceDirectory 'memory.jsonl')))){$processInfo.ArgumentList.Add($argument)}
            $monitor=[Diagnostics.Process]::Start($processInfo)
        }
        for($index=1;$index -le $Count;$index++) {
            $activeItem=$null
            $deadline=(Get-DriverNow).AddSeconds($TimeoutSeconds)
            $endpoint=if($Parameters.Count){'buildWithParameters'}else{'build'}
            $queued=Invoke-BoundedJenkinsRequest POST ([uri]::new($jobUri,$endpoint)) $headers $Parameters
            if([int]$queued.StatusCode -ne 201) { throw "Build submission returned HTTP $($queued.StatusCode); not retried" }
            $location=[string](@($queued.Headers.Location)[0])
            if(-not $location) { throw 'Build submission lacks queue Location; not retried' }
            $queueUri=[uri]::new($baseUri,$location)
            if($queueUri.UserInfo -or $queueUri.Authority -ne $baseUri.Authority -or $queueUri.Scheme -ne $baseUri.Scheme -or $queueUri.AbsolutePath -notmatch '/queue/item/([0-9]+)/?$' -or $queueUri.Query -or $queueUri.Fragment) { throw 'Unsafe Jenkins queue Location; not followed' }
            $queueId=[long]$Matches[1]
            if(@($record.builds | Where-Object queue_id -eq $queueId).Count){throw 'Jenkins reused an already recorded queue item'}
            $item=[ordered]@{queue_id=$queueId;number=$null;result=$null;duration_ms=$null;buildlens_id=$null;error=$null}
            $activeItem=$item
            $record.builds+=,$item
            Save-DriverRecord
            $number=0
            while(-not $number) {
                if((Get-DriverNow) -ge $deadline) { throw "Queue $queueId exceeded build deadline" }
                $q=Convert-JenkinsJson (Invoke-BoundedJenkinsRequest GET ([uri]::new($queueUri,'api/json')) $headers)
                if($q.cancelled) { throw "Queue $queueId was cancelled" }
                if($q.executable) {
                    $number=[int]$q.executable.number
                    if($number -lt 1) { throw 'Queue executable lacks a valid build number' }
                    # Jenkins advertises localhost inside Docker even when the host uses 127.0.0.1.
                    # Construct the trusted build URL ourselves; never follow executable.url.
                } else { Wait-DriverPoll $PollSeconds }
            }
            $item.number=$number
            Save-DriverRecord
            $buildUri=[uri]::new($jobUri,"$number/")
            while($true) {
                if((Get-DriverNow) -ge $deadline) { throw "Build $number exceeded build deadline" }
                $build=Convert-JenkinsJson (Invoke-BoundedJenkinsRequest GET ([uri]::new($buildUri,'api/json')) $headers)
                if([int]$build.number -ne $number -or [long]$build.queueId -ne $queueId) { throw 'Jenkins executable does not match submitted queue item' }
                if($build.building -eq $false) { break }
                Wait-DriverPoll $PollSeconds
            }
            $item.result=$build.result
            $item.duration_ms=$build.duration
            $build | ConvertTo-Json -Depth 12 | Set-Content (Join-Path $EvidenceDirectory "jenkins-$number.json")
            $consoleResponse=Invoke-BoundedJenkinsRequest GET ([uri]::new($buildUri,'consoleText')) $headers
            if([int]$consoleResponse.StatusCode -ne 200){throw 'Cannot capture build console'}
            $console=[string]$consoleResponse.Content
            $console | Set-Content (Join-Path $EvidenceDirectory "console-$number.txt")
            if($console -match 'buildlens report: uploaded .*\{"id":([0-9]+)') {
                $uploadId=[long]$Matches[1]
                if(@($record.builds | Where-Object {$_.number -ne $number -and $_.buildlens_id -eq $uploadId}).Count){throw 'BuildLens upload ID was reused by another executable'}
                $item.buildlens_id=$uploadId
            }
            Save-DriverRecord
            Write-Host "[$index/$Count] $Job #$number $($item.result); BuildLens id $($item.buildlens_id)"
            if($item.result -notin @('SUCCESS','UNSTABLE')) { throw "Build $number completed with $($item.result)" }
            if(-not $item.buildlens_id -and -not $AllowMissingUpload) { throw "Build $number completed without a verified BuildLens upload" }
        }
    } catch {
        $record.error=$_.Exception.Message
        if($activeItem) { $activeItem.error=$record.error }
        throw
    } finally {
        $record.finished_at=(Get-DriverNow).ToString('o')
        Save-DriverRecord
        if($monitor){
            if(-not $monitor.WaitForExit(10000)){Write-Warning 'Memory monitor has not stopped yet; inspect its output before claiming a sampled peak.'}
            elseif($monitor.ExitCode -ne 0){Write-Warning 'Memory monitoring failed; build records are retained.'}
            $monitor.Dispose()
        }
        Write-Host "Verified $($record.verified_uploads)/$Count successful build uploads. Evidence: $EvidenceDirectory"
    }
    return [pscustomobject]$record
}

