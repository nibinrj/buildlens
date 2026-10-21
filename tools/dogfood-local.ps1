#Requires -Version 7.4
# Hashed local adapter for the uncommitted Jenkinsfile; normal multibranch remains an owner-push gate.
$ErrorActionPreference='Stop'
$PSNativeCommandUseErrorActionPreference=$true
$root=Split-Path $PSScriptRoot -Parent
Set-Location $root
$stamp=[datetime]::UtcNow.ToString('yyyyMMdd-HHmmss')
$job="buildlens-dogfood-local-$stamp"
$evidence=Join-Path $root "notes/experiments/$job"
$null=New-Item -ItemType Directory -Force $evidence
$archive=Join-Path $evidence 'source.tar'
tar.exe -cf $archive --exclude=target --exclude=.git --exclude=bin --exclude=dist cmd internal migrations queries testdata shared-library tools go.mod go.sum sqlc.yaml .golangci.yml Dockerfile.ci Jenkinsfile
$sha256=(Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
$sha1=(Get-FileHash -LiteralPath $archive -Algorithm SHA1).Hash.ToLowerInvariant()
$commit=(git rev-parse HEAD).Trim()
$original=[IO.File]::ReadAllText((Join-Path $root 'Jenkinsfile')).Replace("`r`n","`n")
if(-not $original.Contains('deleteDir(); checkout scm')){throw 'Jenkinsfile checkout adapter point changed'}
$snapshot="/tmp/$job"
$body=$original.Substring($original.IndexOf("`n")+1).Replace('deleteDir(); checkout scm',"deleteDir(); sh 'cp -R $snapshot/. .'; echo 'Local source archive SHA256: $sha256'")
$body=$body.Replace("repo: 'nibinrj/buildlens', agentStartedAt:","repo: 'nibinrj/buildlens', commit: '$commit', tree: '$sha1', agentStartedAt:")
$pipeline="@Library('buildlens') _`nwithEnv(['BRANCH_NAME=main','CHANGE_ID=']) {`n"+$body+"`n}`n"
[IO.File]::WriteAllText((Join-Path $evidence 'pipeline.groovy'),$pipeline)
@{job=$job;service_base_commit=$commit;uncommitted=$true;archive_sha256=$sha256;archive_sha1=$sha1;tree_kind='archive content identifier, not a Git tree';jenkinsfile_sha256=(Get-FileHash Jenkinsfile -Algorithm SHA256).Hash.ToLowerInvariant()} | ConvertTo-Json | Set-Content (Join-Path $evidence 'manifest.json')
docker compose exec -T jenkins-agent mkdir -p $snapshot
docker compose cp $archive "jenkins-agent:$snapshot/source.tar"
docker compose exec -T jenkins-agent sh -c 'tar -xf "$1/source.tar" -C "$1"' sh $snapshot
docker compose exec -T jenkins mkdir -p $snapshot
docker compose cp (Join-Path $evidence 'pipeline.groovy') "jenkins:$snapshot/pipeline.groovy"
$bootstrap=@'
import javaposse.jobdsl.dsl.DslScriptLoader
import javaposse.jobdsl.plugin.JenkinsJobManagement
import org.jenkinsci.plugins.scriptsecurity.scripts.ScriptApproval
import org.jenkinsci.plugins.scriptsecurity.scripts.languages.GroovyLanguage
String job = '@JOB@'
String pipeline = new File('/tmp/@JOB@/pipeline.groovy').getText('UTF-8')
ScriptApproval.get().preapprove(pipeline, GroovyLanguage.get())
new DslScriptLoader(new JenkinsJobManagement(System.out, [:], new File('/tmp/@JOB@'))).runScript("""
pipelineJob('${job}') {
  description('Hashed local Jenkinsfile adapter; normal Git-backed verification requires owner push.')
  definition { cps { script(new File('/tmp/${job}/pipeline.groovy').getText('UTF-8')); sandbox(false) } }
}
""")
println "Local dogfood job configured: ${job}"
'@
[IO.File]::WriteAllText((Join-Path $evidence 'bootstrap.groovy'),$bootstrap.Replace('@JOB@',$job))
docker compose cp (Join-Path $evidence 'bootstrap.groovy') "jenkins:$snapshot/bootstrap.groovy"
$script=@'
set -eu
J=http://localhost:8080
A="$JENKINS_ADMIN_USER:$JENKINS_ADMIN_PASSWORD"
C=$(mktemp)
trap 'rm -f "$C"' EXIT
crumb=$(curl -fsS --max-time 20 -u "$A" -c "$C" -b "$C" "$J/crumbIssuer/api/json" | sed -n 's/.*"crumb":"\([^"]*\)".*/\1/p')
curl -fsS --max-time 30 -u "$A" -c "$C" -b "$C" -H "Jenkins-Crumb: $crumb" --data-urlencode script@/tmp/@JOB@/bootstrap.groovy "$J/scriptText"
'@
$script.Replace('@JOB@',$job) | docker compose exec -T jenkins sh -c "tr -d '\r' | sh -s"
& "$PSScriptRoot/drive-builds.ps1" -Count 1 -Job $job -TimeoutSeconds 1200 -EvidenceDirectory (Join-Path $evidence 'run') -MeasureMemory
Write-Host "Local Jenkinsfile evidence: $evidence"
