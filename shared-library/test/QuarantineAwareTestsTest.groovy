import com.lesfurets.jenkins.unit.BasePipelineTest
import org.jenkinsci.plugins.workflow.steps.FlowInterruptedException
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test
import static org.junit.jupiter.api.Assertions.*

class QuarantineAwareTestsTest extends BasePipelineTest {
    def script
    List<String> commands, echoes
    List<Map> junits, catches, outcomes
    Map<String,String> files
    String snapshot
    int fetchStatus, quarantineStatus
    Exception fetchError

    @BeforeEach
    void setupPipeline() {
        super.setUp()
        commands=[]; echoes=[]; junits=[]; catches=[]; outcomes=[]
        files=[:]; snapshot=''; fetchStatus=0; quarantineStatus=0; fetchError=null
        binding.setVariable('env', [:])
        binding.setVariable('currentBuild', [currentResult:'SUCCESS'])
        helper.registerAllowedMethod('pwd', []) { '/workspace/lab' }
        helper.registerAllowedMethod('writeFile', [Map]) { Map m -> files[m.file as String]=m.text }
        helper.registerAllowedMethod('readFile', [Map]) { Map m -> files[m.file as String] }
        helper.registerAllowedMethod('string', [Map]) { Map m -> m }
        helper.registerAllowedMethod('withCredentials', [List,Closure]) { List l,Closure c -> c() }
        helper.registerAllowedMethod('sh', [Map]) { Map m ->
            commands << m.script
            if (m.script.startsWith('buildlens quarantine')) {
                if (fetchError) { throw fetchError }
                files['/workspace/lab/.buildlens/quarantine-excludes.txt']=snapshot
                return fetchStatus
            }
            return m.returnStatus ? quarantineStatus : ''
        }
        helper.registerAllowedMethod('sh', [String]) { String s -> commands << s; return '' }
        helper.registerAllowedMethod('timedStage', [String,Closure]) { String n,Closure c -> c() }
        helper.registerAllowedMethod('timedStage', [String,Map,Closure]) { String n,Map m,Closure c -> c(); outcomes << m }
        helper.registerAllowedMethod('junit', [Map]) { Map m -> junits << m }
        helper.registerAllowedMethod('echo', [String]) { String s -> echoes << s }
        helper.registerAllowedMethod('catchError', [Map,Closure]) { Map m,Closure c ->
            catches << m
            try { c() } catch (FlowInterruptedException e) { throw e } catch (Exception ignored) { }
        }
        helper.registerAllowedMethod('error', [String]) { String s -> throw new IllegalStateException(s) }
        script=loadScript('vars/quarantineAwareTests.groovy')
    }

    @Test void emptyList() {
        def reports=script.call(repo:'owner/lab')
        assertEquals('', reports.quarantineReports)
        assertEquals(1,commands.count { it.startsWith('buildlens quarantine') })
        assertEquals(1,junits.size())
        assertFalse(commands.any { it.contains('surefire.includesFile') })
        assertTrue(commands.any { it.contains("surefire.excludesFile='/workspace/lab/") })
        assertFalse(commands.any { it.contains('flaky.seed') })
    }
    @Test void quarantineFailurePreservesBuildResult() {
        snapshot='lab.RandomTest#random\n'; quarantineStatus=1
        def reports=script.call(repo:'owner/lab',mavenArgs:'-Dsample=7')
        assertEquals('**/target/surefire-reports-quarantine/TEST-*.xml',reports.quarantineReports)
        assertEquals(snapshot,files['/workspace/lab/.buildlens/quarantine-only.txt'])
        assertEquals('SUCCESS',binding.getVariable('currentBuild').currentResult)
        assertEquals('SUCCESS',catches[0].buildResult)
        assertEquals('UNSTABLE',catches[0].stageResult)
        assertFalse(catches[0].catchInterruptions)
        assertTrue(junits[1].skipMarkingBuildUnstable)
        assertEquals('UNSTABLE',outcomes[0].result)
        assertTrue(commands.any { it.contains('-Dbuildlens.reportSuffix=-quarantine') })
    }
    @Test void serverDownClearsStaleList() {
        snapshot='lab.RandomTest#random\n'; fetchStatus=1
        script.call(repo:'owner/lab')
        assertEquals('',files['/workspace/lab/.buildlens/quarantine-only.txt'])
        assertFalse(commands.any { it.contains('surefire.includesFile') })
        assertTrue(echoes.any { it.contains('WARNING') })
    }
    @Test void missingCliAlsoRunsEverything() {
        fetchError=new IllegalStateException('cannot launch')
        script.call(repo:'owner/lab')
        assertEquals('',files['/workspace/lab/.buildlens/quarantine-excludes.txt'])
        assertEquals(1,junits.size())
    }
    @Test void interruptionPropagates() {
        fetchError=new FlowInterruptedException()
        assertThrows(FlowInterruptedException) { script.call(repo:'owner/lab') }
    }
    @Test void earlierUnstableBuildStaysUnstable() {
        binding.getVariable('currentBuild').currentResult='UNSTABLE'
        snapshot='lab.RandomTest#random\n'; quarantineStatus=1
        script.call(repo:'owner/lab')
        assertEquals('UNSTABLE',binding.getVariable('currentBuild').currentResult)
    }
}
