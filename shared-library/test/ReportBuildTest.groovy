import com.lesfurets.jenkins.unit.BasePipelineTest
import groovy.json.JsonSlurper
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test

import static org.junit.jupiter.api.Assertions.assertEquals
import static org.junit.jupiter.api.Assertions.assertFalse
import static org.junit.jupiter.api.Assertions.assertNull
import static org.junit.jupiter.api.Assertions.assertTrue

/** vars/reportBuild.groovy: uploads the build with the CLI and never fails the build. */
class ReportBuildTest extends BasePipelineTest {

    static final String COMMIT = '0123456789abcdef0123456789abcdef01234567'
    static final String TREE = '89abcdef0123456789abcdef0123456789abcdef'

    def reportBuild
    Map<String, String> files          // writeFile calls: path -> text
    List<String> reportCommands        // sh scripts run with returnStatus (the buildlens call)
    List<String> echoes
    List<Map> credentials              // string(...) bindings requested
    Map<String, String> gitAnswers     // git command -> stdout
    int cliStatus
    Integer logLinesAsked
    Closure<Void> writeFileHook

    @BeforeEach
    void setUpPipeline() {
        super.setUp()
        files = [:]
        reportCommands = []
        echoes = []
        credentials = []
        cliStatus = 0
        logLinesAsked = null
        writeFileHook = null
        gitAnswers = [
            'git config --get remote.origin.url': 'https://github.com/nibinrj/buildlens-lab.git\n',
            'git rev-parse HEAD'                : COMMIT + '\n',
            'git rev-parse HEAD^{tree}'         : TREE + '\n',
        ]

        helper.registerAllowedMethod('writeFile', [Map]) { Map m ->
            if (writeFileHook) {
                writeFileHook(m)
            }
            files[m.file as String] = m.text as String
        }
        helper.registerAllowedMethod('echo', [String]) { String s -> echoes << s }
        helper.registerAllowedMethod('string', [Map]) { Map m -> credentials << m; m }
        helper.registerAllowedMethod('withCredentials', [List, Closure]) { List l, Closure body -> body() }
        helper.registerAllowedMethod('sh', [Map]) { Map m ->
            String script = m.script as String
            if (m.returnStdout) {
                String command = script.replace(' 2>/dev/null || true', '')
                return gitAnswers.getOrDefault(command, '')
            }
            reportCommands << script
            return cliStatus
        }

        List<String> log = (1..800).collect { "log line ${it}" }
        binding.setVariable('env', [BUILDLENS_STAGES:
            '{"name":"Build","startedAt":1791115200000,"durationMs":1200,"result":"SUCCESS"}\n' +
            '{"name":"Test","startedAt":1791115201200,"durationMs":3400,"result":"UNSTABLE"}\n'])
        binding.setVariable('currentBuild', [
            currentResult     : 'UNSTABLE',
            startTimeInMillis : 1791115199000L,
            rawBuild          : [getLog: { int n -> logLinesAsked = n; log.takeRight(n) }],
        ])
        reportBuild = loadScript('vars/reportBuild.groovy')
    }

    String theCommand() {
        assertEquals(1, reportCommands.size(), 'exactly one buildlens call')
        return reportCommands[0]
    }

    @Test
    void uploadsStagesLogTailAndBuildFacts() {
        reportBuild.call()

        List stages = new JsonSlurper().parseText(files['.buildlens/stages.json']) as List
        assertEquals(['Build', 'Test'], stages*.name, 'stages.json holds what timedStage recorded')
        assertEquals(500, logLinesAsked, 'asks Jenkins for the last 500 lines')
        assertEquals(500, files['.buildlens/log-tail.txt'].readLines().size())
        assertTrue(files['.buildlens/log-tail.txt'].startsWith('log line 301\n'))

        String cmd = theCommand()
        assertTrue(cmd.startsWith("'buildlens' 'report' "), cmd)
        assertTrue(cmd.contains("'--repo' 'nibinrj/buildlens-lab'"), 'repo parsed from the git remote: ' + cmd)
        assertTrue(cmd.contains("'--result' 'UNSTABLE'"), 'current build result: ' + cmd)
        assertTrue(cmd.contains("'--started-at' '1791115199000'"), cmd)
        assertTrue(cmd.contains("'--commit' '${COMMIT}'"), cmd)
        assertTrue(cmd.contains("'--tree' '${TREE}'"), 'tested tree hash (D-078): ' + cmd)
        assertTrue(cmd.contains("'--stages' '.buildlens/stages.json'"), cmd)
        assertTrue(cmd.contains("'--log-tail' '.buildlens/log-tail.txt'"), cmd)
        assertFalse(cmd.contains('--strict'), cmd)
        assertEquals([[credentialsId: 'buildlens-ingest-key', variable: 'BUILDLENS_KEY']], credentials,
            'the key comes from the Jenkins credential, as an environment variable')
        assertTrue(echoes.isEmpty(), "no warnings: ${echoes}")
    }

    @Test
    void optionsOverrideDefaults() {
        reportBuild.call(repo: 'acme/other', result: 'FAILURE', credentialsId: 'other-key', logLines: 50, strict: true)

        String cmd = theCommand()
        assertTrue(cmd.contains("'--repo' 'acme/other'"), cmd)
        assertTrue(cmd.contains("'--result' 'FAILURE'"), cmd)
        assertTrue(cmd.endsWith("'--strict'"), cmd)
        assertEquals(50, logLinesAsked)
        assertEquals('other-key', credentials[0].credentialsId)
    }

    @Test
    void cliFailureIsOnlyAWarning() {
        cliStatus = 1

        reportBuild.call()

        assertEquals(1, reportCommands.size())
        assertTrue(echoes.any { it.contains('WARNING') && it.contains('exited with 1') }, "${echoes}")
        assertNull(binding.getVariable('currentBuild').result, 'the build result is never touched')
    }

    @Test
    void unexpectedErrorIsCaughtAndReported() {
        writeFileHook = { Map m -> throw new IOException('disk full') }

        reportBuild.call()  // must not throw

        assertTrue(reportCommands.isEmpty(), 'nothing uploaded')
        assertTrue(echoes.any { it.contains('WARNING') && it.contains('disk full') }, "${echoes}")
        assertNull(binding.getVariable('currentBuild').result)
    }

    @Test
    void withoutGitCheckoutCommitAndTreeAreLeftOut() {
        gitAnswers.clear()

        reportBuild.call(repo: 'nibinrj/buildlens-lab')

        String cmd = theCommand()
        assertFalse(cmd.contains('--commit'), 'the CLI then falls back to GIT_COMMIT: ' + cmd)
        assertFalse(cmd.contains('--tree'), cmd)
    }

    @Test
    void noStagesGivesAnEmptyArray() {
        binding.getVariable('env').remove('BUILDLENS_STAGES')

        reportBuild.call()

        assertEquals('[]', files['.buildlens/stages.json'])
    }

    @Test
    void repoNamesFromRemoteUrls() {
        def cases = [
            'https://github.com/nibinrj/buildlens-lab.git': 'nibinrj/buildlens-lab',
            'https://github.com/nibinrj/buildlens-lab'    : 'nibinrj/buildlens-lab',
            'https://github.com/nibinrj/buildlens-lab/'   : 'nibinrj/buildlens-lab',
            'git@github.com:nibinrj/buildlens.git'        : 'nibinrj/buildlens',
            'ssh://git@github.com/acme/my.repo.git'       : 'acme/my.repo',
            ''                                            : '',
            'not a url'                                   : '',
        ]
        cases.each { url, want ->
            assertEquals(want, reportBuild.repoFromRemote(url), "repoFromRemote('${url}')")
        }
    }

    @Test
    void valuesAreShellQuoted() {
        assertEquals("'plain'", reportBuild.shellQuote('plain'))
        assertEquals("'it'\"'\"'s; rm -rf /'", reportBuild.shellQuote("it's; rm -rf /"),
            'a quote in a branch name cannot end the quoting')
    }
}
