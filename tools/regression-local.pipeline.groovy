// Local, hashed snapshot adapter. All duration changes are explicitly INJECTED.
properties([disableConcurrentBuilds(), parameters([
    string(name: 'SLOW_MILLIS', defaultValue: '300', description: 'Controlled injected delay, 0..10000 milliseconds')
])])
node('maven') {
    long agentStartedAt = System.currentTimeMillis()
    timestamps {
        deleteDir()
        sh 'cp -R /tmp/buildlens-regression-snapshot/. .'
        timedStage = load('library/timedStage.groovy')
        reportBuild = load('library/reportBuild.groovy')
        quarantineAwareTests = load('library/quarantineAwareTests.groovy')
        String slow = params.SLOW_MILLIS?.trim() ?: '300'
        if (!(slow ==~ /[0-9]{1,5}/) || slow.toInteger() > 10000) { error('Invalid injected delay') }
        withEnv(['BRANCH_NAME=main', 'CHANGE_ID=']) {
            try {
                timedStage('Checkout') { echo 'Uncommitted regression snapshot: @SNAPSHOT_SHA256@' }
                dir('lab') {
                    timedStage('Build') { sh 'mvn -B -ntp -DskipTests package' }
                    quarantineAwareTests(repo: 'nibinrj/buildlens-lab',
                        mavenArgs: "-Dbuildlens.flaky.seed=5 -Dbuildlens.flaky.clockMillis=5000 -Dbuildlens.slow.ms=${slow}")
                }
            } catch (org.jenkinsci.plugins.workflow.steps.FlowInterruptedException e) {
                currentBuild.result = 'ABORTED'; throw e
            } catch (e) {
                currentBuild.result = 'FAILURE'; throw e
            } finally {
                dir('lab') {
                    reportBuild(repo:'nibinrj/buildlens-lab',commit:'@LAB_BASE_COMMIT@',tree:'@SNAPSHOT_SHA1@',
                        agentStartedAt:agentStartedAt, strict:true,logLines:2000,
                        reports:'**/target/surefire-reports/TEST-*.xml',
                        quarantineReports:'**/target/surefire-reports-quarantine/TEST-*.xml')
                }
            }
        }
    }
}
