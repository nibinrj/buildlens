// Job DSL seed: every job in this Jenkins is defined here.
// Applied by JCasC (jenkins/casc/jenkins.yaml, "jobs:") at every start and by ./tasks.ps1 seed.
//
// Branches and pull requests are found by a periodic scan, not webhooks: GitHub cannot reach a Jenkins running on
// localhost. Pull requests from forks are not built (no fork discovery trait): their code is untrusted.

def repositories = [
    [name: 'buildlens-lab', description: 'Lab Maven project, the build subject (labelled flaky and slow tests arrive in P3/P4).'],
    [name: 'buildlens',     description: 'BuildLens itself (its Jenkinsfile arrives in P5, dogfooding).'],
]

repositories.each { repo ->
    multibranchPipelineJob(repo.name) {
        description(repo.description)
        branchSources {
            branchSource {
                source {
                    github {
                        id(repo.name)
                        repoOwner('nibinrj')
                        repository(repo.name)
                        repositoryUrl("https://github.com/nibinrj/${repo.name}")
                        configuredByUrl(true)
                        credentialsId('github-token')
                        traits {
                            // 1 = build branches that are not also the source of a pull request
                            gitHubBranchDiscovery { strategyId(1) }
                            // 1 = build a pull request merged with its target branch
                            gitHubPullRequestDiscovery { strategyId(1) }
                        }
                    }
                }
            }
        }
        factory {
            workflowBranchProjectFactory { scriptPath('Jenkinsfile') }
        }
        triggers {
            periodicFolderTrigger { interval('15m') }
        }
        orphanedItemStrategy {
            discardOldItems { numToKeep(20) }
        }
    }
}
