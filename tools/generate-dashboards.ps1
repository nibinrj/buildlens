#Requires -Version 7.4
# Query files are the editable source; verify-grafana.ps1 executes generated targets.
$ErrorActionPreference='Stop'
$root=Split-Path $PSScriptRoot -Parent
$destination=Join-Path $root 'grafana/dashboards'
$null=New-Item -ItemType Directory -Force $destination
$source=@{type='grafana-postgresql-datasource';uid='buildlens-postgres'}
function New-Panel([int]$Id,[string]$Title,[string]$Query,[string]$Type,[int]$X,[int]$Y,[int]$W,[int]$H,[string]$Unit='short') {
    $sql=[IO.File]::ReadAllText((Join-Path $root "grafana/queries/$Query.sql")).Replace("`r`n","`n").Trim()
    $panel=[ordered]@{id=$Id;title=$Title;type=$Type;datasource=$source;gridPos=@{x=$X;y=($Y+1);w=$W;h=$H};targets=@(@{refId='A';datasource=$source;editorMode='code';rawQuery=$true;rawSql=$sql;format=$(if($Type -eq 'timeseries'){'time_series'}else{'table'})});fieldConfig=@{defaults=@{unit=$Unit;color=@{mode='palette-classic'}};overrides=@()};options=@{}}
    if($Unit -eq 'currencyUSD'){$panel.fieldConfig.defaults.decimals=6}
    if($Query -in @('cost-branch','cost-stage')){
        $field=if($Query -eq 'cost-branch'){'Equivalent reference USD'}else{'Duration-weighted equivalent USD'}
        $panel.fieldConfig.overrides+=@{matcher=@{id='byName';options=$field};properties=@(@{id='unit';value='currencyUSD'},@{id='decimals';value=6})}
    }
    if($Query -eq 'regression-stage'){
        foreach($entry in @(@('Measured ms','blue'),@('Baseline median','green'),@('Threshold','red'))){
            $panel.fieldConfig.overrides+=@{matcher=@{id='byName';options=$entry[0]};properties=@(@{id='color';value=@{mode='fixed';fixedColor=$entry[1]}},@{id='custom.fillOpacity';value=0})}
        }
    }
    if($Type -eq 'stat'){$panel.options=@{reduceOptions=@{values=$false;calcs=@('lastNotNull');fields=''};orientation='horizontal';textMode='auto';colorMode='value';graphMode='none';justifyMode='auto'}}
    if($Type -eq 'timeseries'){$panel.fieldConfig.defaults.custom=@{lineWidth=2;fillOpacity=8;spanNulls=$false;showPoints='auto'};$panel.options=@{legend=@{displayMode='list';placement='bottom';calcs=@()};tooltip=@{mode='multi';sort='desc'}}}
    if($Type -eq 'table'){$panel.options=@{showHeader=$true;cellHeight='sm';footer=@{show=$false}}}
    return $panel
}
function New-Text([string]$Content) { return @{id=1;type='text';title='Reading this dashboard';gridPos=@{x=0;y=0;w=24;h=4};options=@{mode='markdown';content=$Content}} }
function Write-Dashboard([string]$Slug,[string]$Title,[string]$Description,[array]$Panels,[switch]$Stage) {
    $repository=@{name='repository';label='Repository';type='query';datasource=$source;query='SELECT name AS __text,name AS __value FROM repository ORDER BY name';refresh=1;multi=$false;includeAll=$false;current=@{text='nibinrj/buildlens-lab';value='nibinrj/buildlens-lab'};options=@()}
    $variables=@($repository)
    if($Stage){$variables+=@{name='stage';label='Stage';type='query';datasource=$source;query='SELECT DISTINCT s.name AS __text,s.name AS __value FROM stage_run s JOIN build b ON b.id=s.build_id JOIN repository r ON r.id=b.repository_id WHERE r.name=${repository:sqlstring} ORDER BY 1';refresh=1;current=@{text='Test';value='Test'};options=@();multi=$false;includeAll=$false}}
    $links=@('flaky','regressions','cost','health') | ForEach-Object {@{title=(Get-Culture).TextInfo.ToTitleCase($_);type='link';url="/d/buildlens-$_";keepTime=$true;includeVars=$true;targetBlank=$false}}
    $dashboard=[ordered]@{id=$null;uid="buildlens-$Slug";title="BuildLens / $Title";description=$Description;tags=@('buildlens','local-demo');timezone='utc';schemaVersion=39;version=1;editable=$false;refresh='30s';time=@{from='now-30d';to='now'};timepicker=@{};templating=@{list=$variables};annotations=@{list=@()};links=@($links);panels=$Panels}
    [IO.File]::WriteAllText((Join-Path $destination "$Slug.json"),($dashboard | ConvertTo-Json -Depth 30)+"`n")
}
Write-Dashboard 'flaky' 'Flaky tests' 'Current quarantine and stored detection evidence. Lab flakiness is injected.' @(
    (New-Text '**Injected lab behavior.** Current quarantine ignores the time range; event charts use it. Rule counts are evidence occurrences across activations and cap alerts, not distinct tests or production precision.'),
    (New-Panel 2 'Quarantined now' 'flaky-current' 'stat' 0 3 6 5),
    (New-Panel 3 'Detection evidence by rule' 'flaky-rules' 'table' 6 3 18 5),
    (New-Panel 4 'Quarantine activations and releases' 'flaky-events' 'timeseries' 0 8 24 8),
    (New-Panel 5 'Active quarantine / current state' 'flaky-active' 'table' 0 16 24 8)
)
Write-Dashboard 'regressions' 'Duration regressions' 'Actual durations against stored baselines and thresholds.' @(
    (New-Text '**Stored baseline evidence.** Select a stage. Gaps mean missing metrics or insufficient data. Snapshot copy and GitHub checkout measure different operations and can alert. Controlled slow tests are injected.'),
    (New-Panel 2 'Regression alert rows' 'regression-count' 'stat' 0 3 6 5),
    (New-Panel 3 'Selected stage / measured, median, threshold' 'regression-stage' 'timeseries' 6 3 18 9 'ms'),
    (New-Panel 4 'Latest regression evidence' 'regression-alerts' 'table' 0 12 24 12)
) -Stage
Write-Dashboard 'cost' 'Reference cost' 'Equivalent local compute cost at a dated reference price; never AWS spend.' @(
    (New-Text '**Equivalent reference cost, not AWS spend.** Totals include LOCAL builds only; unknown costs stay null. Stage costs allocate each known build cost by recorded stage duration, not separate metering. Storage, network, idle time and CPU credits are excluded.'),
    (New-Panel 2 'Known local reference cost' 'cost-total' 'stat' 0 3 12 5 'currencyUSD'),
    (New-Panel 3 'Builds with unknown cost / all lifecycles' 'cost-unknown' 'stat' 12 3 12 5),
    (New-Panel 4 'Equivalent cost per build / branch' 'cost-build' 'timeseries' 0 8 24 8 'currencyUSD'),
    (New-Panel 5 'Equivalent cost by branch' 'cost-branch' 'table' 0 16 12 8),
    (New-Panel 6 'Stage cost / duration-weighted allocation' 'cost-stage' 'table' 12 16 12 8)
)
Write-Dashboard 'health' 'Build health' 'Build observations, outcomes and infrastructure attribution.' @(
    (New-Text '**Recorded observations only.** An empty reason table means no matching infrastructure-attributed builds. Infra failures are excluded from flaky rules. Sampled memory and reproducible batch results are recorded in docs/results.md.'),
    (New-Panel 2 'Recorded builds' 'health-count' 'stat' 0 3 12 5),
    (New-Panel 3 'Infrastructure-attributed builds' 'health-infra-count' 'stat' 12 3 12 5),
    (New-Panel 4 'Builds per day / result' 'health-daily' 'timeseries' 0 8 16 8),
    (New-Panel 5 'Infrastructure failures by reason' 'health-infra' 'table' 16 8 8 8),
    (New-Panel 6 'Recent builds' 'health-recent' 'table' 0 16 24 10)
)
Write-Host 'Generated four provisioned dashboards from 17 SQL query files.'
