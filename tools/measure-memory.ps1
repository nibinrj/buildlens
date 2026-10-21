#Requires -Version 7.4
param([Parameter(Mandatory)][string]$RunFile,[Parameter(Mandatory)][string]$OutputFile,[ValidateRange(1,60)][int]$IntervalSeconds=2)
$ErrorActionPreference='Stop'
$PSNativeCommandUseErrorActionPreference=$true
function Convert-DockerMemory([string]$Value) {
    if($Value -notmatch '^([0-9]+(?:\.[0-9]+)?)\s*(B|KiB|MiB|GiB|kB|MB|GB)$'){throw "Unrecognized Docker memory unit: $Value"}
    $amount=[double]::Parse($Matches[1],[Globalization.CultureInfo]::InvariantCulture)
    $factor=switch -CaseSensitive ($Matches[2]) {'B'{1};'KiB'{1024};'MiB'{1048576};'GiB'{1073741824};'kB'{1000};'MB'{1000000};'GB'{1000000000}}
    return $amount*$factor/1048576
}
if($MyInvocation.InvocationName -ne '.') {
    $root=Split-Path $PSScriptRoot -Parent
    Set-Location $root
    $names=@('buildlens-postgres-1','buildlens-grafana-1','buildlens-buildlens-server-1','buildlens-jenkins-1','buildlens-jenkins-agent-1')
    if(Test-Path $OutputFile){throw 'Memory output already exists; use a new file'}
    $deadline=[datetime]::UtcNow.AddHours(2)
    while([datetime]::UtcNow -lt $deadline) {
        $sample=[ordered]@{observed_at=[datetime]::UtcNow.ToString('o');containers=@();total_mib=$null;error=$null}
        try {
            $lines=docker stats --no-stream --format '{{json .}}' @names
            foreach($line in $lines) {
                $row=$line | ConvertFrom-Json
                $usage=$row.MemUsage.Split('/')
                $sample.containers+=@{name=$row.Name;used_mib=(Convert-DockerMemory $usage[0].Trim());limit_mib=(Convert-DockerMemory $usage[1].Trim())}
            }
            if($sample.containers.Count -ne 5){throw 'Incomplete five-container sample'}
            $sample.total_mib=($sample.containers | Measure-Object -Sum used_mib).Sum
        } catch { $sample.error=$_.Exception.Message }
        [IO.File]::AppendAllText([IO.Path]::GetFullPath($OutputFile),($sample | ConvertTo-Json -Depth 5 -Compress)+"`n")
        $run=$null
        try {
            if(Test-Path $RunFile){
                $stream=[IO.File]::Open([IO.Path]::GetFullPath($RunFile),[IO.FileMode]::Open,[IO.FileAccess]::Read,([IO.FileShare]::ReadWrite -bor [IO.FileShare]::Delete))
                $reader=[IO.StreamReader]::new($stream)
                try {$run=$reader.ReadToEnd() | ConvertFrom-Json} finally {$reader.Dispose()}
            }
        } catch { }
        if($run -and $run.finished_at){break}
        Start-Sleep -Seconds $IntervalSeconds
    }
    Write-Host "Memory samples saved to $OutputFile"
}
