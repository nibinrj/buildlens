#Requires -Version 7.4
param([string]$EvidenceDirectory='docs/evidence/P5/restore')
$ErrorActionPreference='Stop'
$PSNativeCommandUseErrorActionPreference=$true
$root=Split-Path $PSScriptRoot -Parent
Set-Location $root
$stamp=[datetime]::UtcNow.ToString('yyyyMMddHHmmss')
$dump="backups/verified-$stamp.dump"
$database="buildlens_restore_$stamp"
$relations=@('repository','build','stage_run','test_case','test_run','quarantine','alert','price','infra_event','pr_comment','goose_db_version')
$parts=@()
foreach($relation in $relations){$parts+="SELECT '$relation' AS relation,count(*) AS rows,md5(COALESCE(string_agg(to_jsonb(t)::text,'|' ORDER BY to_jsonb(t)::text),'')) AS content_fingerprint FROM $relation t"}
$sql="SET TIME ZONE 'UTC'; SELECT jsonb_agg(x ORDER BY relation) FROM ("+($parts -join ' UNION ALL ')+') x;'
function Fingerprint([string]$DatabaseName='') {
    if($DatabaseName){return $sql | docker compose exec -T postgres sh -c 'psql -X -A -t -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$1"' sh $DatabaseName}
    return $sql | docker compose exec -T postgres sh -c 'psql -X -A -t -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
}
# Run after builds finish. If a writer changes data during verification, comparison fails visibly.
$before=(Fingerprint) -join ''
& "$PSScriptRoot/demo-store.ps1" -Mode Dump -Path $dump
& "$PSScriptRoot/demo-store.ps1" -Mode Restore -Path $dump -Database $database
$restored=(Fingerprint $database) -join ''
$after=(Fingerprint) -join ''
if($before -ne $after){throw 'Live data changed during restore verification; rerun after writers finish'}
if($before -ne $restored){throw 'Restored contents differ from the source database'}
$null=New-Item -ItemType Directory -Force $EvidenceDirectory
$before | Set-Content (Join-Path $EvidenceDirectory 'source-fingerprints.json')
$restored | Set-Content (Join-Path $EvidenceDirectory 'restored-fingerprints.json')
@{verified_at=[datetime]::UtcNow.ToString('o');dump_file=$dump;sha256=(Get-FileHash $dump -Algorithm SHA256).Hash.ToLowerInvariant();isolated_database=$database;tables_compared=$relations.Count;contents_match=$true;live_replacement_executed=$false} | ConvertTo-Json | Set-Content (Join-Path $EvidenceDirectory 'verification.json')
Write-Host "Restore verification: all $($relations.Count) table row counts and content fingerprints match in isolated $database."
