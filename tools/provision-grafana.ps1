#Requires -Version 7.4
$ErrorActionPreference='Stop'
$PSNativeCommandUseErrorActionPreference=$true
$root=Split-Path $PSScriptRoot -Parent
Set-Location $root
. "$PSScriptRoot/jenkins-driver.ps1"
$envPath=Join-Path $root '.env'
$settings=Read-BuildLensEnv $envPath
if(-not $settings.GRAFANA_DB_PASSWORD) {
    $password=[Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(24)).ToLowerInvariant()
    $text=[IO.File]::ReadAllText($envPath)
    if($text -match '(?m)^GRAFANA_DB_PASSWORD=.*$'){$text=[regex]::Replace($text,'(?m)^GRAFANA_DB_PASSWORD=.*$',"GRAFANA_DB_PASSWORD=$password")}
    else {$text=$text.TrimEnd()+"`nGRAFANA_DB_PASSWORD=$password`n"}
    [IO.File]::WriteAllText($envPath,$text)
    $settings.GRAFANA_DB_PASSWORD=$password
    Write-Host 'Generated a Grafana reader password in ignored .env.'
}
$previous=$env:GRAFANA_DB_PASSWORD
try {
    $env:GRAFANA_DB_PASSWORD=$settings.GRAFANA_DB_PASSWORD
    Get-Content "$PSScriptRoot/provision-grafana.sql" -Raw | docker compose exec -T -e GRAFANA_DB_PASSWORD postgres sh -c 'psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
} finally { $env:GRAFANA_DB_PASSWORD=$previous }
Write-Host 'Grafana read-only database role provisioned.'
