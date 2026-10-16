// Local verification adapter: same P3 vars, supplied from an uncommitted, hashed snapshot.
properties([disableConcurrentBuilds(), parameters([
    string(name:'LAB_SEED', defaultValue:'', description:'Optional seed for an explicit injected verification case'),
    string(name:'LAB_CLOCK', defaultValue:'', description:'Optional simulated milliseconds'),
    booleanParam(name:'NO_RERUNS', defaultValue:false, description:'Expose a persistent injected quarantine failure')
])])
node('maven') {
    timestamps {
        deleteDir()
        sh 'cp -R /tmp/buildlens-p3-snapshot/. .'
        timedStage = load('library/timedStage.groovy')
        reportBuild = load('library/reportBuild.groovy')
        quarantineAwareTests = load('library/quarantineAwareTests.groovy')
        String seed = params.LAB_SEED?.trim() ?: env.BUILD_NUMBER
        String clock = params.LAB_CLOCK?.trim() ?: "${env.BUILD_NUMBER}000"
        String reruns = params.NO_RERUNS ? ' -Dsurefire.rerunFailingTestsCount=0' : ''
        if (!(seed ==~ /[0-9]+/) || !(clock ==~ /[0-9]+/)) { error('Lab seed/clock must be nonnegative integers') }
        withEnv(['BRANCH_NAME=main']) {
            try {
                timedStage('Checkout') { echo 'P3 local uncommitted snapshot: @SNAPSHOT_SHA256@' }
                dir('lab') {
                    timedStage('Build') { sh 'mvn -B -ntp -DskipTests package' }
                    quarantineAwareTests(repo: 'nibinrj/buildlens-lab',
                        mavenArgs: "-Dbuildlens.flaky.seed=${seed} -Dbuildlens.flaky.clockMillis=${clock}${reruns}")
                }
            } catch (org.jenkinsci.plugins.workflow.steps.FlowInterruptedException e) {
                currentBuild.result = 'ABORTED'; throw e
            } catch (e) {
                currentBuild.result = 'FAILURE'; throw e
            } finally {
                dir('lab') {
                    reportBuild(repo: 'nibinrj/buildlens-lab', commit: '@LAB_BASE_COMMIT@', tree: '@SNAPSHOT_SHA1@',
                        strict: true, logLines: 2000,
                        reports: '**/target/surefire-reports/TEST-*.xml',
                        quarantineReports: '**/target/surefire-reports-quarantine/TEST-*.xml')
                }
            }
        }
    }
}
