import com.lesfurets.jenkins.unit.BasePipelineTest
import groovy.json.JsonSlurper
import org.jenkinsci.plugins.workflow.steps.FlowInterruptedException
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test

import static org.junit.jupiter.api.Assertions.assertEquals
import static org.junit.jupiter.api.Assertions.assertSame
import static org.junit.jupiter.api.Assertions.assertThrows
import static org.junit.jupiter.api.Assertions.assertTrue

/** vars/timedStage.groovy: stage timings recorded for BuildLens. */
class TimedStageTest extends BasePipelineTest {

    def timedStage
    List<String> stagesRun

    @BeforeEach
    void setUpPipeline() {
        super.setUp()
        stagesRun = []
        helper.registerAllowedMethod('stage', [String, Closure]) { String name, Closure body ->
            stagesRun << name
            return body()
        }
        binding.setVariable('env', [:])
        binding.setVariable('currentBuild', [currentResult: 'SUCCESS'])
        timedStage = loadScript('vars/timedStage.groovy')
    }

    /** The records timedStage appended to env.BUILDLENS_STAGES, parsed. */
    List<Map> records() {
        String lines = binding.getVariable('env').BUILDLENS_STAGES ?: ''
        return lines.readLines().findAll { it }.collect { new JsonSlurper().parseText(it) as Map }
    }

    @Test
    void successfulStageIsRecorded() {
        long before = System.currentTimeMillis()
        def value = timedStage.call('Build') { 'body result' }

        assertEquals('body result', value, 'the body value passes through')
        assertEquals(['Build'], stagesRun, 'wraps a real stage()')
        List<Map> recs = records()
        assertEquals(1, recs.size())
        assertEquals('Build', recs[0].name)
        assertEquals('SUCCESS', recs[0].result)
        assertTrue((recs[0].startedAt as long) >= before, 'startedAt is epoch milliseconds at the start')
        assertTrue((recs[0].durationMs as long) >= 0)
    }

    @Test
    void failingStageIsRecordedAndRethrown() {
        def boom = new IllegalStateException('mvn failed')
        def thrown = assertThrows(IllegalStateException) {
            timedStage.call('Test') { throw boom }
        }

        assertSame(boom, thrown, 'the original exception reaches the pipeline')
        assertEquals('FAILURE', records()[0].result)
    }

    @Test
    void abortedBuildIsRecordedAsAborted() {
        assertThrows(FlowInterruptedException) {
            timedStage.call('Deploy') { throw new FlowInterruptedException() }
        }
        assertEquals('ABORTED', records()[0].result)
    }

    @Test
    void stageThatMakesTheBuildUnstableIsUnstable() {
        timedStage.call('Test') { binding.getVariable('currentBuild').currentResult = 'UNSTABLE' }
        timedStage.call('Package') { 'ok' }

        List<Map> recs = records()
        assertEquals('UNSTABLE', recs[0].result, 'the stage where junit found failures')
        assertEquals('SUCCESS', recs[1].result, 'a later stage is not blamed for an earlier unstable result')
    }

    @Test
    void stagesAreAppendedInOrder() {
        ['Checkout', 'Build', 'Test'].each { name -> timedStage.call(name) { 'ok' } }

        assertEquals(['Checkout', 'Build', 'Test'], records()*.name)
    }

    @Test
    void existingRecordsAreKept() {
        binding.getVariable('env').BUILDLENS_STAGES = '{"name":"Earlier","startedAt":1,"durationMs":2,"result":"SUCCESS"}\n'
        timedStage.call('Later') { 'ok' }

        assertEquals(['Earlier', 'Later'], records()*.name)
    }
}
