#Requires -Version 7.4
[CmdletBinding(SupportsShouldProcess,ConfirmImpact='High')]
param(
    [Parameter(Mandatory)][ValidateSet('Dump','Restore')][string]$Mode,
    [string]$Path='',
    [ValidatePattern('^buildlens_restore_[a-z0-9_]+$')][string]$Database=('buildlens_restore_'+[datetime]::UtcNow.ToString('yyyyMMddHHmmss')),
    [switch]$ReplaceLive
)
$ErrorActionPreference='Stop'
$PSNativeCommandUseErrorActionPreference=$true
$root=Split-Path $PSScriptRoot -Parent
Set-Location $root
. "$PSScriptRoot/jenkins-driver.ps1"
$settings=Read-BuildLensEnv (Join-Path $root '.env')
$containerPath='/tmp/buildlens-demo-'+[guid]::NewGuid().ToString('N')+'.dump'
if($Mode -eq 'Dump') {
    if(-not $Path){$Path='backups/buildlens-'+[datetime]::UtcNow.ToString('yyyyMMdd-HHmmss')+'.dump'}
    $full=[IO.Path]::GetFullPath($Path)
    if([IO.Path]::GetExtension($full) -ne '.dump'){throw 'Use a .dump file (ignored by Git)'}
    if(Test-Path -LiteralPath $full){throw 'Dump destination already exists'}
    $null=New-Item -ItemType Directory -Force (Split-Path $full)
    try {
        docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc -f "$1"' sh $containerPath
        docker compose cp "postgres:$containerPath" $full
        $manifest=@{created_at=[datetime]::UtcNow.ToString('o');database=$settings.POSTGRES_DB;sha256=(Get-FileHash -LiteralPath $full -Algorithm SHA256).Hash.ToLowerInvariant();format='PostgreSQL custom archive';path=$full}
        $manifest | ConvertTo-Json | Set-Content -LiteralPath ($full+'.json')
    } finally { docker compose exec -T postgres rm -f -- $containerPath }
    Write-Host "Database dump saved to $full"
    return
}
if(-not $Path -or -not (Test-Path -LiteralPath $Path -PathType Leaf)){throw 'Restore requires an existing .dump file'}
$full=(Resolve-Path -LiteralPath $Path).Path
if(Test-Path -LiteralPath ($full+'.json')) {
    $manifest=Get-Content -LiteralPath ($full+'.json') -Raw | ConvertFrom-Json
    if((Get-FileHash -LiteralPath $full -Algorithm SHA256).Hash.ToLowerInvariant() -ne $manifest.sha256){throw 'Dump checksum mismatch'}
}
if($ReplaceLive -and -not $PSCmdlet.ShouldProcess($settings.POSTGRES_DB,'Replace live database contents from the demo archive (clears pending comment work)')){return}
$stopped=$false
try {
    docker compose cp $full "postgres:$containerPath"
    docker compose exec -T postgres pg_restore --list $containerPath | Out-Null
    if($ReplaceLive) {
        # The owner must explicitly select this path; automated verification restores into a clone.
        docker compose stop jenkins-agent buildlens-server
        $stopped=$true
        docker compose exec -T postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists --single-transaction --exit-on-error --no-owner "$1"' sh $containerPath
        # Restoring analytics must not replay historical external comment writes.
        'TRUNCATE pr_comment;' | docker compose exec -T postgres sh -c 'psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
        & "$PSScriptRoot/provision-grafana.ps1"
        Write-Host 'Live demo history restored; operational comment outbox cleared.'
    } else {
        docker compose exec -T postgres sh -c 'createdb -U "$POSTGRES_USER" -T template0 "$1"' sh $Database
        docker compose exec -T postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d "$1" --single-transaction --exit-on-error --no-owner --no-privileges "$2"' sh $Database $containerPath
        Write-Host "Restored isolated database $Database. Active demo data is unchanged."
    }
} finally {
    docker compose exec -T postgres rm -f -- $containerPath
    if($stopped){docker compose up -d --wait buildlens-server jenkins-agent}
}
