#Requires -Version 7.4
$ErrorActionPreference='Stop'
. "$PSScriptRoot/../jenkins-driver.ps1"
$testBase=Join-Path (Split-Path (Split-Path $PSScriptRoot -Parent) -Parent) 'notes/experiments'
$testRoot=Join-Path $testBase ('buildlens-driver-tests-'+[guid]::NewGuid().ToString('N'))
$null=New-Item -ItemType Directory $testRoot
$envFile=Join-Path $testRoot 'test.env'
"JENKINS_ADMIN_USER=test`nJENKINS_API_TOKEN=synthetic-test-only`nJENKINS_URL=http://localhost:8080/`n" | Set-Content $envFile
function Get-DriverNow { return $script:clock }
function Wait-DriverPoll([int]$Seconds) { $script:clock=$script:clock.AddSeconds($Seconds) }
function Invoke-JenkinsRequest([string]$Method,[uri]$Uri,[hashtable]$Headers,[hashtable]$Body=@{},[int]$RequestTimeout=20) {
    $content='{}'; $status=200; $responseHeaders=@{}
    if($Method -eq 'POST') {
        $script:submissions++
        if($script:mode -eq 'password success' -and $Headers['Jenkins-Crumb'] -ne 'synthetic-crumb'){throw 'Password submission lacks CSRF crumb'}
        if($script:mode -eq 'uncertain'){throw 'Synthetic transport failure'}
        $status=201; $responseHeaders.Location='http://localhost:8080/queue/item/7/'
        if($script:mode -eq 'unauthorized'){$status=401}
        if($script:mode -eq 'missing location'){$responseHeaders=@{}}
        if($script:mode -eq 'foreign location'){$responseHeaders.Location='https://example.invalid/queue/item/7/'}
    } elseif($Uri.AbsolutePath -match '/crumbIssuer/') {
        $content='{"crumbRequestField":"Jenkins-Crumb","crumb":"synthetic-crumb"}'
    } elseif($Uri.AbsolutePath -match '/queue/') {
        if($script:mode -eq 'cancelled'){$content='{"cancelled":true}'}
        elseif($script:mode -eq 'queue timeout'){$content='{"cancelled":false}'}
        elseif($script:mode -eq 'queue expired'){$status=404}
        elseif($script:mode -eq 'malformed JSON'){$content='{'}
        else {$content='{"cancelled":false,"executable":{"number":42,"url":"https://example.invalid/do-not-follow"}}'}
    } elseif($Uri.AbsolutePath.EndsWith('consoleText')) {
        $content='buildlens report: uploaded test #42 with 1 report file(s): {"id":900,"created":true}'
        if($script:mode -eq 'missing upload'){$content='build completed without upload'}
    } else {
        $content='{"number":42,"queueId":7,"building":false,"result":"SUCCESS","duration":1234}'
        if($script:mode -eq 'wrong queue'){$content='{"number":42,"queueId":8,"building":false,"result":"SUCCESS"}'}
        if($script:mode -eq 'failed build'){$content='{"number":42,"queueId":7,"building":false,"result":"FAILURE"}'}
        if($script:mode -eq 'build timeout'){$content='{"number":42,"queueId":7,"building":true}'}
    }
    return [pscustomobject]@{StatusCode=$status;Content=$content;Headers=$responseHeaders}
}
$cases=@(
    @{name='success';error=''},
    @{name='password success';error=''},
    @{name='unauthorized';error='HTTP 401'},
    @{name='missing location';error='lacks queue Location'},
    @{name='foreign location';error='Unsafe Jenkins queue'},
    @{name='cancelled';error='cancelled'},
    @{name='queue expired';error='HTTP 404'},
    @{name='queue timeout';error='deadline'},
    @{name='build timeout';error='deadline'},
    @{name='malformed JSON';error='invalid JSON'},
    @{name='wrong queue';error='does not match'},
    @{name='failed build';error='FAILURE'},
    @{name='missing upload';error='without a verified'},
    @{name='uncertain';error='transport failure'}
    @{name='duplicate queue';error='reused an already recorded queue'}
)
try {
    foreach($case in $cases) {
        $script:mode=$case.name; $script:submissions=0; $script:clock=[datetime]'2026-10-19T00:00:00Z'
        if($case.name -eq 'password success'){"JENKINS_ADMIN_USER=test`nJENKINS_ADMIN_PASSWORD=synthetic-password`nJENKINS_URL=http://localhost:8080/`n" | Set-Content $envFile}
        else {"JENKINS_ADMIN_USER=test`nJENKINS_API_TOKEN=synthetic-test-only`nJENKINS_URL=http://localhost:8080/`n" | Set-Content $envFile}
        $out=Join-Path $testRoot ($case.name.Replace(' ','-'))
        $caught=''
        $requested=if($case.name -eq 'duplicate queue'){2}else{1}
        try { $result=Invoke-BuildDriver -Count $requested -Job 'folder/lab branch' -Parameters @{SLOW_MILLIS='300'} -TimeoutSeconds 2 -PollSeconds 1 -EnvFile $envFile -EvidenceDirectory $out -UsePassword:($case.name -eq 'password success') }
        catch {$caught=$_.Exception.Message}
        if($case.error -and $caught -notlike "*$($case.error)*"){throw "$($case.name): expected '$($case.error)', got '$caught'"}
        if(-not $case.error -and $caught){throw "$($case.name): unexpected $caught"}
        if($script:submissions -ne $requested){throw "$($case.name): POST repeated or skipped"}
        $record=Get-Content (Join-Path $out 'run.json') -Raw | ConvertFrom-Json
        if(-not $record.finished_at){throw "$($case.name): final checkpoint missing"}
        if($case.error -and -not $record.error){throw "$($case.name): failure checkpoint missing"}
        if(-not $case.error -and $record.builds[0].buildlens_id -ne 900){throw 'Upload ID not preserved'}
        if($case.name -eq 'duplicate queue' -and $record.verified_uploads -ne 1){throw 'Previously verified partial progress was lost'}
        Write-Host "PASS $($case.name)"
    }
    foreach($badJob in @('../lab','folder//lab','https://host/job','lab?x')) {
        try { Invoke-BuildDriver -Count 1 -Job $badJob -Parameters @{} -TimeoutSeconds 2 -PollSeconds 1 -EnvFile $envFile -EvidenceDirectory (Join-Path $testRoot 'invalid') | Out-Null; throw 'Unsafe job accepted' }
        catch {if($_.Exception.Message -eq 'Unsafe job accepted'){throw}}
    }
    Write-Host "Driver tests: $($cases.Count) request scenarios and 4 invalid job paths passed."
} finally {
    # The test generated this exact GUID directory; no user files are removed.
    $resolved=[IO.Path]::GetFullPath($testRoot)
    if($resolved.StartsWith([IO.Path]::GetFullPath($testBase)+[IO.Path]::DirectorySeparatorChar) -and (Split-Path $resolved -Leaf) -match '^buildlens-driver-tests-[a-f0-9]{32}$') { Remove-Item -LiteralPath $resolved -Recurse -Force }
}
