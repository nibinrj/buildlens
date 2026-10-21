import groovy.lang.Binding
import groovy.lang.GroovyShell
import org.jenkinsci.plugins.workflow.steps.FlowInterruptedException
import org.junit.jupiter.api.Test
import static org.junit.jupiter.api.Assertions.*

/** Execute the real Jenkinsfile body with mocked steps; library retrieval is verified live. */
class DogfoodPipelineTest {
    Map execute(String failureAt = '', Exception failure = new IllegalStateException('injected command failure')) {
        List stages = []
        List commands = []
        List reports = []
        Map build = [result: null]
        Binding context = new Binding([
            currentBuild: build, scm: [test: true],
            disableConcurrentBuilds: { [:] }, properties: { List ignored -> },
            timeout: { Map limits, Closure body -> assertEquals(20, limits.time); body() },
            node: { String label, Closure body -> assertEquals('go', label); body() },
            timestamps: { Closure body -> body() },
            timedStage: { String name, Closure body -> stages << name; if (failureAt == name) throw failure; body() },
            deleteDir: { }, checkout: { Object ignored -> },
            sh: { String command -> commands << command },
            reportBuild: { Map options -> reports << options },
        ])
        String source = new File('../Jenkinsfile').getText('UTF-8')
        assertTrue(source.startsWith("@Library('buildlens') _"))
        // The unit run replaces only the remote library annotation; no pipeline body is rewritten.
        source = source.substring(source.indexOf('\n') + 1)
        Exception thrown = null
        try { new GroovyShell(getClass().classLoader, context).evaluate(source, 'DogfoodJenkinsfile') }
        catch (Exception e) { thrown = e }
        [stages: stages, commands: commands, reports: reports, build: build, thrown: thrown]
    }
    @Test void checksAndReportsSuccessfulBuild() {
        Map result = execute()
        assertNull(result.thrown)
        assertEquals(['Checkout', 'Static binaries', 'Go checks', 'Library tests'], result.stages)
        assertTrue(result.commands.any { it.contains('CGO_ENABLED=0 go build -p 1') })
        assertTrue(result.commands.contains('sh tools/ci.sh'))
        assertTrue(result.commands.any { it.contains('mvn -B -ntp test') })
        assertEquals(1, result.reports.size())
        assertEquals('nibinrj/buildlens', result.reports[0].repo)
        assertTrue(result.reports[0].agentStartedAt > 0)
    }
    @Test void failureStopsLaterChecksAndStillReports() {
        Map result = execute('Go checks')
        assertEquals('FAILURE', result.build.result)
        assertNotNull(result.thrown)
        assertFalse(result.stages.contains('Library tests'))
        assertEquals(1, result.reports.size())
    }
    @Test void checkoutFailureStillReports() {
        Map result = execute('Checkout')
        assertEquals(['Checkout'], result.stages)
        assertEquals('FAILURE', result.build.result)
        assertEquals(1, result.reports.size())
    }
    @Test void interruptionRemainsAborted() {
        Map result = execute('Go checks', new FlowInterruptedException())
        assertEquals('ABORTED', result.build.result)
        assertTrue(result.thrown instanceof FlowInterruptedException)
        assertEquals(1, result.reports.size())
    }
}
