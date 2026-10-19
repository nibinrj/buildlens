import javaposse.jobdsl.dsl.DslScriptLoader
import javaposse.jobdsl.plugin.JenkinsJobManagement
import org.jenkinsci.plugins.scriptsecurity.scripts.ScriptApproval
import org.jenkinsci.plugins.scriptsecurity.scripts.languages.GroovyLanguage

String pipeline = new File('/tmp/buildlens-regression/pipeline.groovy').getText('UTF-8')
String job = new File('/tmp/buildlens-regression/job-name.txt').getText('UTF-8').trim()
if (!(job ==~ /buildlens-regression-local-[0-9]{8}-[0-9]{6}/)) { throw new IllegalArgumentException('Invalid local job name') }
ScriptApproval.get().preapprove(pipeline, GroovyLanguage.get())
def management = new JenkinsJobManagement(System.out, [:], new File('/tmp/buildlens-regression'))
new DslScriptLoader(management).runScript("""
pipelineJob('${job}') {
    description('Injected regression verification from an uncommitted, hashed local snapshot.')
    parameters { stringParam('SLOW_MILLIS', '300', 'Injected delay in milliseconds') }
    definition { cps { script(new File('/tmp/buildlens-regression/pipeline.groovy').getText('UTF-8')); sandbox(false) } }
}
""")
println "Regression local job configured by Job DSL: ${job}"
