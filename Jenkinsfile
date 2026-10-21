@Library('buildlens') _
properties([disableConcurrentBuilds()])
timeout(time: 20, unit: 'MINUTES') {
    node('go') {
        long agentStartedAt = System.currentTimeMillis()
        timestamps {
            try {
                timedStage('Checkout') { deleteDir(); checkout scm }
                timedStage('Static binaries') {
                    sh 'mkdir -p bin && GOMAXPROCS=2 GOMEMLIMIT=1200MiB CGO_ENABLED=0 go build -p 1 -trimpath -o bin/buildlens ./cmd/buildlens && GOMAXPROCS=2 GOMEMLIMIT=1200MiB CGO_ENABLED=0 go build -p 1 -trimpath -o bin/buildlens-server ./cmd/buildlens-server'
                }
                timedStage('Go checks') { sh 'sh tools/ci.sh' }
                timedStage('Library tests') { sh 'cd shared-library && mvn -B -ntp test' }
            } catch (org.jenkinsci.plugins.workflow.steps.FlowInterruptedException e) {
                currentBuild.result = 'ABORTED'; throw e
            } catch (e) {
                currentBuild.result = 'FAILURE'; throw e
            } finally {
                reportBuild(repo: 'nibinrj/buildlens', agentStartedAt: agentStartedAt,
                    reports: 'shared-library/target/surefire-reports/TEST-*.xml')
            }
        }
    }
}
