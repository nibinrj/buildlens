#!/bin/sh
# Waits until the controller is up and has written this agent's secret into the shared volume
# (jenkins/controller/init.groovy.d/agent-secret.groovy), then starts the standard inbound agent.
# The agent never needs the admin password.
set -eu

secret_file="${BUILDLENS_AGENT_SECRET_FILE:-/buildlens-agent/secret}"
tries=0
until [ -s "$secret_file" ] && curl -fsS -o /dev/null "${JENKINS_URL%/}/login"; do
    tries=$((tries + 1))
    if [ "$tries" -gt 120 ]; then
        echo "buildlens agent: no controller or no secret after 10 minutes, giving up" >&2
        exit 1
    fi
    sleep 5
done

JENKINS_SECRET="$(cat "$secret_file")"
export JENKINS_SECRET
echo "buildlens agent: secret found, connecting to ${JENKINS_URL} as ${JENKINS_AGENT_NAME}"
exec /usr/local/bin/jenkins-agent "$@"
