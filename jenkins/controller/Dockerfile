# syntax=docker/dockerfile:1
# Jenkins controller: pinned LTS, pinned plugins, configured only by JCasC (jenkins/casc/jenkins.yaml).
# Build context: jenkins/ (see docker-compose.yml). Versions checked 2026-10-04 (docs/study/P2-jenkins-cli-ingest.md).

FROM jenkins/jenkins:2.580.1-lts-jdk21@sha256:a660310e39ade10631f774bacd5219de767dfd08947ea5c32a739b5e5bb382c1

# No setup wizard: JCasC creates the admin user and everything else.
ENV JAVA_OPTS="-Djenkins.install.runSetupWizard=false"

# Plugins are installed at build time, every version pinned, so the image never changes underneath us.
COPY --chown=jenkins:jenkins controller/plugins.txt /usr/share/jenkins/ref/plugins.txt
RUN jenkins-plugin-cli --plugin-file /usr/share/jenkins/ref/plugins.txt

# Start-up script that hands the inbound agent its secret through a shared volume (no admin password on the
# agent). Files in ref/ are copied into JENKINS_HOME at start; ".override" makes a changed script replace the old copy.
COPY --chown=jenkins:jenkins controller/init.groovy.d/agent-secret.groovy /usr/share/jenkins/ref/init.groovy.d/agent-secret.groovy.override

# The shared volume's mount point must belong to the jenkins user (uid 1000), so the empty volume inherits that.
USER root
RUN mkdir -p /buildlens-agent && chown jenkins:jenkins /buildlens-agent && chmod 750 /buildlens-agent
USER jenkins
