// Runs at every controller start (Jenkins executes $JENKINS_HOME/init.groovy.d/*.groovy after start-up).
//
// An inbound agent proves who it is with a secret derived from the controller's private key, known only after the
// controller has started. Instead of giving the agent admin credentials to download it (builds could then read
// them), this writes the secret into a volume that only the agent container mounts, read-only.
// The node itself is defined in jenkins/casc/jenkins.yaml.

import hudson.slaves.SlaveComputer
import jenkins.model.Jenkins

String agentName = System.getenv('BUILDLENS_AGENT_NAME') ?: 'agent-1'
File dir = new File(System.getenv('BUILDLENS_AGENT_SECRET_DIR') ?: '/buildlens-agent')

def computer = Jenkins.get().getComputer(agentName)
if (!(computer instanceof SlaveComputer)) {
    println "buildlens: no inbound agent named '${agentName}'; check jenkins/casc/jenkins.yaml"
    return
}

// Write to a temporary file and rename it, so the agent never reads a half-written secret.
File tmp = new File(dir, 'secret.tmp')
tmp.text = computer.jnlpMac
tmp.setReadable(false, false)
tmp.setReadable(true, true)
if (!tmp.renameTo(new File(dir, 'secret'))) {
    println "buildlens: could not write the agent secret into ${dir}"
    return
}
println "buildlens: agent secret for '${agentName}' written to ${dir}"
