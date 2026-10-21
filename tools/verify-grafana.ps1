#Requires -Version 7.4
param([string]$EvidenceDirectory='notes/experiments/grafana-verification',[string]$Repository='nibinrj/buildlens-lab')
$ErrorActionPreference='Stop'
$root=Split-Path $PSScriptRoot -Parent
Set-Location $root
. "$PSScriptRoot/jenkins-driver.ps1"
$settings=Read-BuildLensEnv (Join-Path $root '.env')
$port=if($settings.GRAFANA_HOST_PORT){$settings.GRAFANA_HOST_PORT}else{'3001'}
$base="http://localhost:$port"
$basic=[Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($settings.GRAFANA_ADMIN_USER+':'+$settings.GRAFANA_ADMIN_PASSWORD))
$headers=@{Authorization="Basic $basic"}
$null=New-Item -ItemType Directory -Force $EvidenceDirectory
function Grafana([string]$Path,[string]$Method='GET',$Body=$null) {
    $options=@{Uri=$base+$Path;Method=$Method;Headers=$headers;TimeoutSec=20;MaximumRedirection=0;ErrorAction='Stop'}
    if($Body){$options.ContentType='application/json';$options.Body=($Body | ConvertTo-Json -Depth 25 -Compress)}
    try {return Invoke-RestMethod @options} catch {throw "Grafana request failed: $Method $Path"}
}
$ds=Grafana '/api/datasources/uid/buildlens-postgres'
if($ds.user -ne 'buildlens_grafana' -or -not $ds.readOnly -or -not $ds.secureJsonFields.password){throw 'Datasource is not provisioned with the expected reader credential'}
$health=Grafana '/api/datasources/uid/buildlens-postgres/health'
if($health.status -ne 'OK'){throw 'Postgres datasource health failed'}
$health | ConvertTo-Json -Depth 8 | Set-Content (Join-Path $EvidenceDirectory 'datasource-health.json')
$results=@()
foreach($file in Get-ChildItem grafana/dashboards/*.json) {
    $expected=Get-Content $file.FullName -Raw | ConvertFrom-Json
    $loaded=Grafana "/api/dashboards/uid/$($expected.uid)"
    if(-not $loaded.meta.provisioned){throw "$($expected.uid) is not file-provisioned"}
    $loaded | ConvertTo-Json -Depth 40 | Set-Content (Join-Path $EvidenceDirectory "$($expected.uid).json")
    $queries=@()
    foreach($panel in $expected.panels){foreach($target in $panel.targets){$queries+=@{name=$panel.title;query=$target;panel=$panel.id}}}
    foreach($variable in $expected.templating.list){$queries+=@{name="Variable: $($variable.name)";panel=0;query=@{refId='A';datasource=@{uid='buildlens-postgres';type='grafana-postgresql-datasource'};rawSql=$variable.query;format='table';rawQuery=$true}}}
    $index=0
    foreach($entry in $queries){
        $index++
        $query=$entry.query | ConvertTo-Json -Depth 15 | ConvertFrom-Json -AsHashtable
        $query.rawSql=$query.rawSql.Replace('${repository:sqlstring}',("'"+$Repository.Replace("'","''")+"'")).Replace('${stage:sqlstring}',"'Test'")
        $query.intervalMs=60000; $query.maxDataPoints=1000
        $payload=@{from=[DateTimeOffset]::UtcNow.AddDays(-30).ToUnixTimeMilliseconds().ToString();to=[DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds().ToString();queries=@($query)}
        $response=Grafana '/api/ds/query' 'POST' $payload
        $answer=$response.results.A
        if(-not $answer -or $answer.error -or ($answer.status -and $answer.status -ne 200)){throw "$($expected.uid)/$($entry.name) query failed"}
        $response | ConvertTo-Json -Depth 40 | Set-Content (Join-Path $EvidenceDirectory "$($expected.uid)-query-$index.json")
        $rows=0
        foreach($frame in $answer.frames){if($frame.data.values.Count){$rows+=$frame.data.values[0].Count}}
        $results+=@{dashboard=$expected.uid;name=$entry.name;rows=$rows;status='PASS'}
        Write-Host "PASS $($expected.uid) / $($entry.name) ($rows rows)"
    }
}
$results | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $EvidenceDirectory 'queries.json')
Write-Host "Grafana: four provisioned dashboards; $($results.Count) panel/variable queries passed."
