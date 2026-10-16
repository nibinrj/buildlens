package infra

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct{ name, log, reason string }{
		{"agent deletion", "14:12:30 Agent agent-1 was deleted; cancelling node body", "AGENT_LOST"},
		{"channel closed", "hudson.remoting.ChannelClosedException: Channel closed", "AGENT_LOST"},
		{"agent offline timeout", "agent-1 has been removed or offline for 5 min; assuming it is not coming back, and terminating shell step", "AGENT_LOST"},
		{"OOM", "container State: OOMKilled=true", "OUT_OF_MEMORY"},
		{"exit code", "ERROR: script returned exit code 137", "OUT_OF_MEMORY"},
		{"exit status", "process exited with exit status 137", "OUT_OF_MEMORY"},
		{"disk", "[ERROR] java.io.IOException: No space left on device", "DISK_FULL"},
		{"OS disk diagnostic", "mkdir: cannot create directory: No space left on device", "DISK_FULL"},
		{"docker", "[ERROR] org.testcontainers.DockerClientFactory: Cannot connect to the Docker daemon at unix:///var/run/docker.sock", "DOCKER_UNAVAILABLE"},
		{"testcontainers unavailable", "java.lang.IllegalStateException: Could not find a valid Docker environment", "DOCKER_UNAVAILABLE"},
		{"dependency timeout", "[ERROR] Could not transfer artifact com.acme:lib:pom:1 from central: Connect timed out", "DEPENDENCY_TIMEOUT"},
		{"healthy", "[INFO] BUILD SUCCESS", ""},
		{"near miss assertion", "[ERROR] java.lang.AssertionError: expected message No space left on device", ""},
		{"near miss Docker assertion", "[ERROR] AssertionFailedError: Cannot connect to the Docker daemon", ""},
		{"near miss quoted sample", "example: script returned exit code 137", ""},
		{"OOM flag false", "container: OOMKilled=false, exitCode=0", ""},
		{"bare number", "test stdout: 137", ""},
		{"test timeout", "[ERROR] TimeoutException: test request timed out", ""},
		{"artifact 404", "[ERROR] Could not transfer artifact a:b:1: HTTP 404", ""},
		{"mere offline", "test stdout: agent is offline", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.log)
			if got.Reason != tt.reason {
				t.Fatalf("reason=%q want %q, match=%+v", got.Reason, tt.reason, got)
			}
			if got.Failure() != (tt.reason != "") {
				t.Error("wrong failure flag")
			}
		})
	}
}
