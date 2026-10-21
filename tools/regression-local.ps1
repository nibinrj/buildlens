#Requires -Version 7.4
<#
Runs actual local Jenkins builds with injected durations and no PR publication.
Uses the existing stack and reviewed Job DSL; never changes Git state.
#>
param(
    [ValidateRange(0,50)][int]$BaselineBuilds=13,
    [ValidateRange(0,10000)][int]$BaselineMillis=300,
    [ValidateRange(0,10000)][int]$SlowMillis=3000,
    [string]$LabPath=(Join-Path (Split-Path $PSScriptRoot -Parent) '..\buildlens-lab'),
    [switch]$PrepareOnly
)
$ErrorActionPreference='Stop'
$PSNativeCommandUseErrorActionPreference=$true
if($BaselineBuilds -gt 0 -and $BaselineBuilds -lt 13){throw 'BaselineBuilds must be zero (reuse stored history) or at least 13'}
$root=Split-Path $PSScriptRoot -Parent
Set-Location $root
$stamp=(Get-Date).ToUniversalTime().ToString('yyyyMMdd-HHmmss')
$job="buildlens-regression-local-$stamp"
$evidence=Join-Path $root "notes/experiments/P4-$stamp"
$snapshot=Join-Path $evidence 'snapshot'
New-Item -ItemType Directory -Force -Path (Join-Path $snapshot 'library'),(Join-Path $snapshot 'lab') | Out-Null
function In-Jenkins([string]$script) {
    $script | docker compose exec -T jenkins sh -c "tr -d '\r' | sh -s"
}
$archive=Join-Path $evidence 'lab.tar'
tar.exe -cf $archive --exclude=target --exclude=.git -C $LabPath pom.xml README.md Jenkinsfile core api flaky-lab
if ((tar.exe -tf $archive) | Where-Object {$_ -match '(^|/)(target|\.git)(/|$)'}) {throw 'Snapshot includes generated output or Git metadata'}
tar.exe -xf $archive -C (Join-Path $snapshot 'lab')
foreach($name in @('timedStage','reportBuild','quarantineAwareTests')) {
    [IO.File]::WriteAllText((Join-Path $snapshot "library/$name.groovy"),[IO.File]::ReadAllText((Join-Path $root "shared-library/vars/$name.groovy"))+"`nreturn this`n")
}
$bundle=Join-Path $evidence 'snapshot.tar'
tar.exe -cf $bundle -C $snapshot library lab
$sha256=(Get-FileHash -LiteralPath $bundle -Algorithm SHA256).Hash.ToLowerInvariant()
$sha1=(Get-FileHash -LiteralPath $bundle -Algorithm SHA1).Hash.ToLowerInvariant()
$labBase=(git -C $LabPath rev-parse HEAD).Trim()
$pipeline=[IO.File]::ReadAllText((Join-Path $PSScriptRoot 'regression-local.pipeline.groovy')).Replace('@SNAPSHOT_SHA256@',$sha256).Replace('@SNAPSHOT_SHA1@',$sha1).Replace('@LAB_BASE_COMMIT@',$labBase)
[IO.File]::WriteAllText((Join-Path $evidence 'pipeline.groovy'),$pipeline)
[IO.File]::WriteAllText((Join-Path $evidence 'job-name.txt'),$job)
$manifest=[ordered]@{date=(Get-Date).ToUniversalTime().ToString('o');job=$job;snapshot_sha256=$sha256;snapshot_sha1=$sha1;lab_base_commit=$labBase;service_base_commit=(git rev-parse HEAD).Trim();uncommitted=$true;baseline_builds=$BaselineBuilds;baseline_ms=$BaselineMillis;slow_ms=$SlowMillis;files=@()}
$sources=@(Get-ChildItem (Join-Path $root 'cmd'),(Join-Path $root 'internal'),(Join-Path $root 'queries'),(Join-Path $root 'migrations'),(Join-Path $root 'shared-library/vars') -Recurse -File)
$sources+=Get-Item (Join-Path $root 'config.yaml'),(Join-Path $root 'go.mod'),(Join-Path $root 'go.sum')
foreach($file in $sources){$manifest.files+=@{path=[IO.Path]::GetRelativePath($root,$file.FullName);sha256=(Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()}}
$manifest | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $evidence 'manifest.json')
docker compose exec -T jenkins-agent mkdir -p /tmp/buildlens-regression-snapshot
docker cp "$snapshot/." buildlens-jenkins-agent-1:/tmp/buildlens-regression-snapshot/
docker compose exec -T --user root jenkins-agent chown -R jenkins:jenkins /tmp/buildlens-regression-snapshot
docker compose exec -T jenkins mkdir -p /tmp/buildlens-regression
docker cp (Join-Path $evidence 'pipeline.groovy') buildlens-jenkins-1:/tmp/buildlens-regression/pipeline.groovy
docker cp (Join-Path $evidence 'job-name.txt') buildlens-jenkins-1:/tmp/buildlens-regression/job-name.txt
docker cp (Join-Path $PSScriptRoot 'regression-local-bootstrap.groovy') buildlens-jenkins-1:/tmp/buildlens-regression/bootstrap.groovy
$configured=In-Jenkins @'
set -eu
J=http://localhost:8080
A="$JENKINS_ADMIN_USER:$JENKINS_ADMIN_PASSWORD"
C=$(mktemp)
trap 'rm -f "$C"' EXIT
crumb=$(curl -fsS --max-time 30 -u "$A" -c "$C" -b "$C" "$J/crumbIssuer/api/json" | sed -n 's/.*"crumb":"\([^"]*\)".*/\1/p')
curl -fsS --max-time 60 -u "$A" -c "$C" -b "$C" -H "Jenkins-Crumb: $crumb" --data-urlencode script@/tmp/buildlens-regression/bootstrap.groovy "$J/scriptText"
mkdir -p /tmp/buildlens-regression/evidence
'@
if(-not ($configured -match 'Regression local job configured by Job DSL')){throw "Job DSL did not confirm configuration: $($configured | Select-Object -First 3)"}
$configured | Write-Host
if($PrepareOnly){Write-Host "Prepared measurement job: $job; source manifest: $evidence/manifest.json"; return}
In-Jenkins "mkdir -p /tmp/buildlens-regression/evidence/$job"
$buildScript=@'
set -eu
J=http://localhost:8080
A="$JENKINS_ADMIN_USER:$JENKINS_ADMIN_PASSWORD"
P="$J/job/@JOB@"
C=$(mktemp)
trap 'rm -f "$C"' EXIT
crumb=$(curl -fsS --max-time 30 -u "$A" -c "$C" -b "$C" "$J/crumbIssuer/api/json" | sed -n 's/.*"crumb":"\([^"]*\)".*/\1/p')
n=$(curl -fsS --max-time 30 -u "$A" "$P/api/json?tree=nextBuildNumber" | sed -n 's/.*"nextBuildNumber":\([0-9]*\).*/\1/p')
curl -fsS --max-time 30 -u "$A" -c "$C" -b "$C" -H "Jenkins-Crumb: $crumb" --data-urlencode 'SLOW_MILLIS=@SLOW@' "$P/buildWithParameters" -o /dev/null
attempt=0
while :; do
    status=$(curl -s --max-time 30 -u "$A" "$P/$n/api/json?tree=building,result" || true)
    case "$status" in *'"building":false'*) break;; esac
    attempt=$((attempt+1))
    if [ "$attempt" -ge 200 ]; then echo "Build $n timed out" >&2; exit 1; fi
    sleep 3
done
curl -fsS --max-time 30 -u "$A" "$P/$n/consoleText" > "/tmp/buildlens-regression/evidence/console-$n.txt"
if ! grep -q 'buildlens report: uploaded' "/tmp/buildlens-regression/evidence/console-$n.txt"; then echo "Build $n did not upload" >&2; exit 1; fi
id=$(sed -n 's/.*buildlens report: uploaded .*{"id":\([0-9]*\).*/\1/p' "/tmp/buildlens-regression/evidence/console-$n.txt" | tail -1)
test -n "$id"
curl -fsS --max-time 30 -H "Authorization: Bearer $BUILDLENS_INGEST_KEY" "http://buildlens-server:8080/api/v1/builds/$id" > "/tmp/buildlens-regression/evidence/build-$n.json"
printf '%s\t%s\t%s\t%s\n' "$n" "$id" '@SLOW@' "$status" >> /tmp/buildlens-regression/evidence/builds.tsv
echo "Injected regression build $n: $status; delay @SLOW@ ms; BuildLens id $id"
'@
$buildScript=$buildScript.Replace('/tmp/buildlens-regression/evidence/',"/tmp/buildlens-regression/evidence/$job/")
for($index=1;$index -le ($BaselineBuilds+2);$index++){
    $delay=if($index -le $BaselineBuilds){$BaselineMillis}else{$SlowMillis}
    In-Jenkins ($buildScript.Replace('@JOB@',$job).Replace('@SLOW@',"$delay"))
    # Preserve each completed build even if the next build fails.
    docker cp "buildlens-jenkins-1:/tmp/buildlens-regression/evidence/$job/." "$evidence/"
}
Write-Host "Actual Jenkins evidence saved to $evidence"
