import javaposse.jobdsl.dsl.DslScriptLoader
import javaposse.jobdsl.plugin.JenkinsJobManagement
import org.jenkinsci.plugins.scriptsecurity.scripts.ScriptApproval
import org.jenkinsci.plugins.scriptsecurity.scripts.languages.GroovyLanguage

// Approve only this reviewed local verification pipeline, never all pending scripts/signatures.
String pipeline = new File('/tmp/buildlens-p3/pipeline.groovy').getText('UTF-8')
ScriptApproval.get().preapprove(pipeline, GroovyLanguage.get())
def management = new JenkinsJobManagement(System.out, [:], new File('/tmp/buildlens-p3'))
new DslScriptLoader(management).runScript("""
pipelineJob('buildlens-p3-local-20261012') {
    description('P3 verification of an uncommitted source snapshot. Configured by tools/p3-local.ps1 and Job DSL.')
    parameters {
        stringParam('LAB_SEED', '', 'Optional injected seed')
        stringParam('LAB_CLOCK', '', 'Optional simulated milliseconds')
        booleanParam('NO_RERUNS', false, 'Expose an injected persistent quarantine failure')
    }
    definition {
        cps {
            script(new File('/tmp/buildlens-p3/pipeline.groovy').getText('UTF-8'))
            sandbox(false)
        }
    }
}
""")
println 'P3 local job configured by Job DSL'
