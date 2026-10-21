#Requires -Version 7.4
param([Parameter(Mandatory)][string]$RunFile,[Parameter(Mandatory)][string]$MemoryFile,[string]$EvidenceDirectory='docs/evidence/P5')
$ErrorActionPreference='Stop'
$PSNativeCommandUseErrorActionPreference=$true
$root=Split-Path $PSScriptRoot -Parent
Set-Location $root
$run=Get-Content $RunFile -Raw | ConvertFrom-Json
if(-not $run.finished_at -or $run.error -or $run.builds.Count -lt 50){throw 'A completed error-free batch of at least 50 builds is required'}
$ids=@($run.builds | ForEach-Object {if(-not $_.buildlens_id -or $_.result -notin @('SUCCESS','UNSTABLE')){throw 'Unverified build in batch'};[long]$_.buildlens_id})
if(@($ids | Select-Object -Unique).Count -ne $ids.Count){throw 'Duplicate upload IDs in batch'}
$started=([datetime]$run.started_at).ToUniversalTime().ToString('o')
$finished=([datetime]$run.finished_at).ToUniversalTime().ToString('o')
$sql=[IO.File]::ReadAllText((Join-Path $PSScriptRoot 'results.sql')).Replace('@BATCH_IDS@',($ids -join ',')).Replace('@STARTED@',$started).Replace('@FINISHED@',$finished)
$json=$sql | docker compose exec -T postgres sh -c 'psql -X -A -t -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
$data=$json | ConvertFrom-Json
if($data.batch.recorded -ne $run.requested){throw 'Database batch count differs from driver checkpoint'}
$null=New-Item -ItemType Directory -Force $EvidenceDirectory
$data | ConvertTo-Json -Depth 18 | Set-Content (Join-Path $EvidenceDirectory 'results.json')
$data.builds | Export-Csv -NoTypeInformation (Join-Path $EvidenceDirectory 'builds.csv')
$data.regression_evidence | ConvertTo-Json -Depth 18 | Set-Content (Join-Path $EvidenceDirectory 'regressions.json')
$data.quarantine_history | ConvertTo-Json -Depth 18 | Set-Content (Join-Path $EvidenceDirectory 'quarantine-history.json')
$run | ConvertTo-Json -Depth 12 | Set-Content (Join-Path $EvidenceDirectory 'driver-run.json')
$samples=@(Get-Content $MemoryFile | ForEach-Object {$_ | ConvertFrom-Json})
$valid=@($samples | Where-Object {$null -ne $_.total_mib -and -not $_.error})
if(-not $valid.Count){throw 'No complete memory samples'}
$peak=$valid | Sort-Object total_mib -Descending | Select-Object -First 1
$gaps=@();for($i=1;$i -lt $samples.Count;$i++){$gaps+=(([datetime]$samples[$i].observed_at)-([datetime]$samples[$i-1].observed_at)).TotalSeconds}
$memory=[ordered]@{samples=$samples.Count;valid_samples=$valid.Count;errors=@($samples | Where-Object error).Count;first_sample=([datetime]$samples[0].observed_at).ToUniversalTime().ToString('o');last_sample=([datetime]$samples[-1].observed_at).ToUniversalTime().ToString('o');start_delay_seconds=(([datetime]$samples[0].observed_at)-([datetime]$run.started_at)).TotalSeconds;maximum_gap_seconds=($gaps | Measure-Object -Maximum).Maximum;peak_sum_mib=$peak.total_mib;peak_at=([datetime]$peak.observed_at).ToUniversalTime().ToString('o');configured_limit_mib=3392;containers=@()}
foreach($name in @('buildlens-postgres-1','buildlens-grafana-1','buildlens-buildlens-server-1','buildlens-jenkins-1','buildlens-jenkins-agent-1')){
    $values=@($valid | ForEach-Object {$_.containers | Where-Object name -eq $name})
    $memory.containers+=@{name=$name;sampled_peak_mib=($values | Measure-Object -Maximum used_mib).Maximum}
}
$memory | ConvertTo-Json -Depth 6 | Set-Content (Join-Path $EvidenceDirectory 'memory-summary.json')
Copy-Item -LiteralPath $MemoryFile -Destination (Join-Path $EvidenceDirectory 'memory.jsonl')
$rows=[Collections.Generic.List[string]]::new()
$rows.Add('# Local measurement results')
$rows.Add('')
$rows.Add('Measured on 2026-10-19. Lab flakiness and controlled delays are **injected**. These are actual local Jenkins runs, not production precision or performance benchmarks.')
$rows.Add('')
$rows.Add('## New batch')
$rows.Add('')
$rows.Add("Job: ``$($run.job)``. UTC interval: $started to $finished. Driver requested $($run.requested) builds and verified every queue/executable pair and BuildLens upload. The initial failed preflight is separate from this completed batch.")
$rows.Add('')
$rows.Add('| Observation | Measured result |')
$rows.Add('| --- | ---: |')
foreach($entry in @(
    @('Recorded batch builds',$data.batch.recorded),@('SUCCESS',$data.batch.successful),@('UNSTABLE',$data.batch.unstable),
    @('BuildLens ID range',"$($data.batch.first_id)-$($data.batch.last_id)"),@('Infrastructure-attributed builds',$data.batch.infra_failures),
    @('Known / unknown cost builds',"$($data.batch.known_cost_builds) / $($data.batch.unknown_cost_builds)"),@('Equivalent reference USD',$data.batch.equivalent_usd),
    @('Equivalent USD per build / min, median, max',"$($data.batch.cost_min_usd), $($data.batch.cost_median_usd), $($data.batch.cost_max_usd)"),
    @('Agent busy milliseconds / sum',$data.batch.agent_busy_ms),@('Stored build duration ms / min, median, max',"$($data.batch.duration_min_ms), $($data.batch.duration_median_ms), $($data.batch.duration_max_ms)"),
    @('Quarantine activations during interval',$data.quarantine.activated_in_batch),@('Quarantine releases during interval',$data.quarantine.released_in_batch),@('Active quarantine at export',$data.quarantine.active_at_export)
)){$rows.Add("| $($entry[0]) | $($entry[1]) |")}
$rows.Add('')
$rows.Add("Historical lab total at export: **$($data.repository_totals.recorded_builds)** stored builds; **$($data.quarantine.historical_activations)** quarantine activations and **$($data.quarantine.historical_releases)** releases. These totals include earlier experiments and are separate from the new batch.")
$rows.Add('')
$rows.Add('## Test observations and rule evidence')
$rows.Add('')
$rows.Add('| Stage | Outcome | Observations |')
$rows.Add('| --- | --- | ---: |')
foreach($observation in $data.test_observations){$rows.Add("| $($observation.stage) | $($observation.outcome) | $($observation.observations) |")}
$rows.Add('')
$rows.Add('| Rule | Evidence occurrences during batch |')
$rows.Add('| --- | ---: |')
foreach($rule in $data.rule_evidence){$rows.Add("| $($rule.rule) | $($rule.evidence_occurrences) |")}
if(-not $data.rule_evidence.Count){$rows.Add('| No new detection events | 0 |')}
$rows.Add('')
$rows.Add('Rule counts combine automatic activations and cap alerts, counting each retained rule finding. They are not distinct tests or accuracy scores. A release can concern a quarantine activated before this batch.')
$rows.Add('')
$rows.Add('## Regression and infrastructure observations')
$rows.Add('')
$rows.Add('| Kind | Metric | Alert rows in new batch |')
$rows.Add('| --- | --- | ---: |')
foreach($alert in $data.regressions){$rows.Add("| $($alert.kind) | $($alert.name.Replace('|','&#124;')) | $($alert.alert_rows) |")}
if(-not $data.regressions.Count){$rows.Add('| None | None | 0 |')}
$rows.Add('')
$rows.Add("The batch retained a fixed requested delay of 300 ms yet generated **$(($data.regressions | Measure-Object -Sum alert_rows).Sum)** regression alert rows. These include incidental timing shifts and comparisons with older history. No new deliberate slowdown was introduced in this batch, and no production accuracy claim is inferred. Exact per-alert baseline/current values are retained in the evidence.")
$rows.Add('')
if(-not $data.infra_reasons.Count){$rows.Add('No infrastructure failure reason was recorded in this batch. Rule fixtures and unit tests are separate evidence; no live infrastructure fault injection is claimed.')}
else{foreach($reason in $data.infra_reasons){$rows.Add("- $($reason.reason): $($reason.builds) builds.")}}
$rows.Add('')
$rows.Add('## Sampled memory')
$rows.Add('')
$rows.Add("Maximum observed simultaneous sum: **$([Math]::Round($memory.peak_sum_mib,2)) MiB**, at $($memory.peak_at), across $($memory.valid_samples) complete samples ($($memory.errors) incomplete/error samples). Configured limits remain **3392 MiB**. Sampling began $([Math]::Round($memory.start_delay_seconds,2)) seconds after batch start; the largest observed sample gap was $([Math]::Round($memory.maximum_gap_seconds,2)) seconds.")
$rows.Add('')
$rows.Add('| Container | Maximum observed MiB |')
$rows.Add('| --- | ---: |')
foreach($container in $memory.containers){$rows.Add("| $($container.name) | $([Math]::Round($container.sampled_peak_mib,2)) |")}
$rows.Add('')
$rows.Add('Individual maxima occur at different times and must not be added as a simultaneous peak. Docker-reported memory is sampled; a short peak or the initial sampling gap can be missed. Unrelated kind clusters are excluded. Dashboard queries and image preparation shared this host; this was not an isolated latency benchmark. Go race checks ran after the batch.')
$rows.Add('')
$rows.Add('## Injected vs real')
$rows.Add('')
$rows.Add('- **Injected:** seeded flaky behavior in OrderTest/RandomTest/TimeTest and the requested 300 ms controlled delay. Earlier regression demonstrations used larger injected delays.')
$rows.Add('- **Real:** Jenkins execution/results, XML observations, quarantine decisions, stored duration/cost calculations, actual GitHub comment delivery in the prior live gate, and Docker memory samples.')
$rows.Add('- **Source identity:** this batch uses a dated, hashed local lab/library snapshot adapter. Its archive identifier is not a Git tree object. The live PR builds separately verified normal Git-backed checkout. See the source manifest and console excerpts in the evidence. An earlier fifty-build measurement is retained separately under evidence/P5/initial-batch/.')
$rows.Add('- **Cost:** equivalent local compute cost at the dated reference price, not AWS spend. Unknown historical costs remain null. Stage dashboard cost is a duration-weighted allocation, not independently metered billing.')
$rows.Add('')
$rows.Add('## Reproduce and inspect')
$rows.Add('')
$rows.Add('Use `./tasks.ps1 up`, configure the local driver token, then `./tasks.ps1 drive 50 <job> SLOW_MILLIS=300`. This batch used the named local snapshot job above; reapply its reviewed bootstrap after a Jenkins restart. A fresh Git-backed run can target `buildlens-lab/main` after the owner publishes the intended lab code. Changing source/job changes the experiment; durations are not expected to repeat exactly.')
$rows.Add('')
$rows.Add('[Reviewed evidence](evidence/P5/README.md) includes the exact driver run, database JSON/CSV, source identity, memory samples, dashboard query checks, dump/restore checks and final test outputs. [Live PR evidence](evidence/P4/live-20261019/README.md) records the actual two-build sticky comment.')
$rows.Add('')
$rows.Add('## Claims still requiring separate verification')
$rows.Add('')
$rows.Add('Production precision/recall, naturally occurring regressions, sustained-load throughput, multi-agent accounting, real AWS costs/deployment, IDoFT benchmark results and the later MCP workflow remain unverified. The new Jenkinsfile needs an owner commit/push for normal multibranch discovery; a hashed local execution does not close that Git gate. Isolated restore does not prove the owner-only live database replacement path.')
[IO.File]::WriteAllText((Join-Path $root 'docs/results.md'),($rows -join "`n")+"`n")
Write-Host "Exported $($data.batch.recorded) verified builds and measured results to docs/results.md"
