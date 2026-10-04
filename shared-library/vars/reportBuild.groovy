import com.cloudbees.groovy.cps.NonCPS

/**
 * Uploads this build to BuildLens with the buildlens CLI. Call it last, inside a node: in a scripted pipeline's
 * finally block, or in a Declarative post { always }. It never changes the build result: any problem is printed
 * as a warning.
 *
 *   reportBuild()
 *   reportBuild(repo: 'nibinrj/buildlens-lab', result: 'FAILURE', strict: false)
 *
 * Options:
 *   repo           owner/name; default: parsed from the git remote "origin"
 *   result         SUCCESS, UNSTABLE, FAILURE, ABORTED; default: currentBuild.currentResult
 *   credentialsId  secret-text credential with the ingest key; default 'buildlens-ingest-key'
 *   logLines       how many log lines to send; default 500
 *   strict         pass --strict to the CLI (it then exits non-zero on failure; still only a warning here)
 *
 * The CLI reads the server URL and the agent lifecycle from the agent's environment
 * (BUILDLENS_SERVER_URL, BUILDLENS_AGENT_LIFECYCLE) and the job, build number, branch and PR from Jenkins's own
 * variables, so they are not passed here.
 */
def call(Map args = [:]) {
    try {
        upload(args)
    } catch (e) {
        echo "BuildLens: WARNING: build not reported (${e}); the build result is unchanged"
    }
}

private void upload(Map args) {
    String dir = '.buildlens'
    int logLines = (args.logLines ?: 500) as int

    // The log so far; the final "Finished: ..." line does not exist yet at this point.
    List<String> log = currentBuild.rawBuild.getLog(logLines)
    writeFile file: "${dir}/log-tail.txt", text: log.join('\n') + '\n'
    writeFile file: "${dir}/stages.json", text: stagesJson(env.BUILDLENS_STAGES)

    String repo = args.repo ?: repoFromRemote(gitOutput('git config --get remote.origin.url'))
    String commit = gitOutput('git rev-parse HEAD')
    // The tree hash identifies the tested content even when Jenkins builds a PR as a fresh merge commit (D-078).
    String tree = gitOutput('git rev-parse HEAD^{tree}')
    String result = args.result ?: currentBuild.currentResult

    List<String> cmd = ['buildlens', 'report',
                        '--repo', repo,
                        '--result', result,
                        '--started-at', "${currentBuild.startTimeInMillis}",
                        '--stages', "${dir}/stages.json",
                        '--log-tail', "${dir}/log-tail.txt"]
    if (commit) {
        cmd += ['--commit', commit]
    }
    if (tree) {
        cmd += ['--tree', tree]
    }
    if (args.strict) {
        cmd << '--strict'
    }

    // The key reaches the CLI only as the BUILDLENS_KEY variable; Jenkins masks it in the log.
    withCredentials([string(credentialsId: args.credentialsId ?: 'buildlens-ingest-key', variable: 'BUILDLENS_KEY')]) {
        int status = sh(script: cmd.collect { shellQuote(it as String) }.join(' '), returnStatus: true)
        if (status != 0) {
            echo "BuildLens: WARNING: buildlens report exited with ${status}; the build result is unchanged"
        }
    }
}

/** Turns the JSON lines timedStage collected into a JSON array. */
@NonCPS
static String stagesJson(String lines) {
    List<String> records = (lines ?: '').readLines().findAll { it.trim() }
    return '[' + records.join(',') + ']'
}

/** "https://github.com/owner/name.git" or "git@github.com:owner/name.git" gives "owner/name"; otherwise "". */
@NonCPS // pure function, no steps; also keeps the regex Matcher out of the serialized program state
static String repoFromRemote(String url) {
    def m = (url ?: '').trim() =~ /[:\/]([^\/:]+)\/([^\/]+?)(\.git)?\/?$/
    return m.find() ? "${m.group(1)}/${m.group(2)}" : ''
}

/** Runs a git command and returns its output, or "" if it fails (for example: no git checkout). */
private String gitOutput(String command) {
    return sh(script: "${command} 2>/dev/null || true", returnStdout: true).trim()
}

/** Single-quotes a value for sh, so branch names or repo names cannot inject shell syntax. */
@NonCPS
static String shellQuote(String s) {
    return "'" + s.replace("'", "'\"'\"'") + "'"
}
