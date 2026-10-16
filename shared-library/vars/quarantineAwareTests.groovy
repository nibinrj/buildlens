/**
 * Fetches one quarantine snapshot, runs blocking tests with exclusions, then quarantined tests.
 * The consumer POM must bind buildlens.reportSuffix to Surefire's reportsDirectory.
 */
def call(Map args = [:]) {
    String repo = args.repo ?: reportBuild.repoFromRemote(gitOutput('git config --get remote.origin.url'))
    String dir = args.dir ?: '.buildlens'
    String excludes = "${pwd()}/${dir}/quarantine-excludes.txt"
    String includes = "${pwd()}/${dir}/quarantine-only.txt"
    String options = args.mavenArgs ?: ''
    String blockingReports = '**/target/surefire-reports/TEST-*.xml'
    String quarantineReports = '**/target/surefire-reports-quarantine/TEST-*.xml'
    writeFile file: excludes, text: ''
    writeFile file: includes, text: ''
    try {
        withCredentials([string(credentialsId: args.credentialsId ?: 'buildlens-ingest-key', variable: 'BUILDLENS_KEY')]) {
            int status = sh(script: "buildlens quarantine --repo ${shellQuote(repo)} --format surefire-excludes --output ${shellQuote(excludes)}", returnStatus: true)
            if (status != 0) {
                writeFile file: excludes, text: ''
                echo "BuildLens: WARNING: quarantine CLI exited ${status}; all tests remain blocking"
            }
        }
    } catch (e) {
        if (e.getClass().name == 'org.jenkinsci.plugins.workflow.steps.FlowInterruptedException') { throw e }
        writeFile file: excludes, text: ''
        echo 'BuildLens: WARNING: quarantine lookup failed; all tests remain blocking'
    }
    String patterns = readFile(file: excludes)
    writeFile file: includes, text: patterns
    echo "BuildLens: blocking exclusions: ${patterns.trim() ?: '(empty; all tests run)'}"
    timedStage(args.blockingStage ?: 'Test') {
        try {
            sh "mvn -B -ntp ${options} -Dmaven.test.failure.ignore=true -Dbuildlens.reportSuffix= -Dsurefire.excludesFile=${shellQuote(excludes)} verify"
        } finally {
            junit allowEmptyResults: true, testResults: blockingReports
        }
    }
    if (!patterns.trim()) {
        echo 'BuildLens: no quarantined tests; skipping non-blocking quarantine stage'
        return [blockingReports: blockingReports, quarantineReports: '']
    }
    Map outcome = [result: 'SUCCESS']
    timedStage(args.quarantineStage ?: 'Quarantine', outcome) {
        catchError(buildResult: 'SUCCESS', stageResult: 'UNSTABLE', catchInterruptions: false, message: 'quarantined test failed') {
            try {
                echo "BuildLens: quarantine-only tests: ${patterns.trim()}"
                int status = sh(script: "mvn -B -ntp ${options} -Dmaven.test.failure.ignore=false -Dbuildlens.reportSuffix=-quarantine -Dsurefire.includesFile=${shellQuote(includes)} test", returnStatus: true)
                junit allowEmptyResults: true, skipMarkingBuildUnstable: true, testResults: quarantineReports
                if (status != 0) { error('one or more quarantined tests failed') }
            } catch (e) {
                outcome.result = 'UNSTABLE'
                throw e
            }
        }
    }
    return [blockingReports: blockingReports, quarantineReports: quarantineReports]
}

private String gitOutput(String command) {
    return sh(script: "${command} 2>/dev/null || true", returnStdout: true).trim()
}

@com.cloudbees.groovy.cps.NonCPS
static String shellQuote(String value) {
    return "'" + (value ?: '').replace("'", "'\"'\"'") + "'"
}
