package org.jenkinsci.plugins.workflow.steps

/**
 * Test stand-in for the Pipeline plugin's exception of the same name, thrown when a build is aborted.
 * timedStage recognises it by class name, so this stub is enough to test the ABORTED path without plugin jars.
 */
class FlowInterruptedException extends InterruptedException {
    FlowInterruptedException() {
        super('aborted by the user (test stub)')
    }
}
