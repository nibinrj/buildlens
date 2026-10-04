import groovy.json.JsonOutput

/**
 * Runs body as a pipeline stage and records the stage's start, duration and result for BuildLens.
 *
 *   timedStage('Build') { sh 'mvn -B -ntp package' }
 *
 * Records are appended to env.BUILDLENS_STAGES, one JSON object per line; reportBuild writes them to stages.json.
 * Pipeline Groovy runs on one thread per build and only switches between parallel branches at step boundaries,
 * and setting env is not a step, so parallel stages cannot lose each other's records.
 *
 * Result: SUCCESS; FAILURE if body throws; ABORTED if the build was interrupted; UNSTABLE if the build became
 * unstable during this stage (for example the junit step found failures). The body's exception is rethrown.
 */
def call(String name, Closure body) {
    return stage(name) {
        long start = System.currentTimeMillis()
        String resultBefore = currentBuild.currentResult
        String result = 'SUCCESS'
        try {
            def value = body()
            if (resultBefore != 'UNSTABLE' && currentBuild.currentResult == 'UNSTABLE') {
                result = 'UNSTABLE'
            }
            return value
        } catch (e) {
            // Matched by name: FlowInterruptedException comes from a plugin jar the unit tests do not load.
            result = e.getClass().name == 'org.jenkinsci.plugins.workflow.steps.FlowInterruptedException' ? 'ABORTED' : 'FAILURE'
            throw e
        } finally {
            long duration = System.currentTimeMillis() - start
            String line = JsonOutput.toJson([name: name, startedAt: start, durationMs: duration, result: result])
            env.BUILDLENS_STAGES = (env.BUILDLENS_STAGES ?: '') + line + '\n'
        }
    }
}
