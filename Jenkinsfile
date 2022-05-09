#!/usr/bin/env groovy

void debugKind(boolean xsite, String... contexts) {
    for (String context : contexts) {
        sh "kubectl config use-context ${context}"
        sh 'kubectl get events --all-namespaces'
        sh 'kubectl cluster-info'
        if (xsite) {
            sh "kubectl config get-contexts -o name | grep -q ${context} && kubectl logs daemonset/speaker -n metallb-system --context ${context} || true"
            sh "kubectl config get-contexts -o name | grep -q ${context} && kubectl logs deployment/controller -n metallb-system --context ${context} || true"
        }
    }
    sh 'df -h'
    sh 'docker ps -a'
    sh 'for log in $(docker ps -qa | xargs); do docker logs --tail 500 $log; done'
}


pipeline {
    agent {
        label 'slave-group-k8s'
    }

    environment {
        GO111MODULE = 'on'
        KUBECONFIG = "$WORKSPACE/kind-kube-config.yaml"
        TESTING_NAMESPACE = 'namespace-for-testing'
        WATCH_NAMESPACE = 'namespace-for-testing'
        PATH="/opt/go/bin:$PATH"
        RUN_SA_OPERATOR = 'true'
        MAKE_DATADIR_WRITABLE = 'true'
        CONFIG_LISTENER_IMAGE = 'localhost:5000/infinispan-operator'
        SERVER_IMAGE = 'quay.io/infinispan/server:13.0'
        TEST_REPORT_DIR = "$WORKSPACE/test/reports"
    }

    options {
        timeout(time: 120, unit: 'MINUTES')
        timestamps()
        buildDiscarder(logRotator(numToKeepStr: '100', daysToKeepStr: '61'))
    }

    stages {
        stage('Build') {
            steps {
                sh 'go mod vendor'
                sh 'make lint'
            }
        }

        stage('Test') {
            steps {
                sh 'make test'
            }
        }

        stage('E2E') {
            environment {
                CHANGE_TARGET = "${env.CHANGE_TARGET}"
            }
            when {
                expression {
                    return !env.BRANCH_NAME.startsWith('PR-') || sh(script: 'git fetch origin $CHANGE_TARGET && git diff --name-only FETCH_HEAD | grep -qvE \'(\\.md$)|(^(documentation|test-integration|.gitignore))/\'', returnStatus: true) == 0
                }
            }
            stages {
                stage('Prepare') {
                    steps{
                        sh 'kind delete clusters --all'
                        sh 'cleanup.sh'
                        // Ensure that we always have the latest version of the server image locally
                        sh "docker pull $SERVER_IMAGE"
                        sh 'make go-junit-report'
                    }
                }

                // stage('Create k8s Cluster') {
                //     steps {
                //         sh 'scripts/ci/kind-with-olm.sh'
                //         writeFile file: "${KUBECONFIG}", text: sh(script: 'kind get kubeconfig', , returnStdout: true)

                //         script {
                //             env.TESTING_CONTEXT = sh(script: 'kubectl --insecure-skip-tls-verify config current-context', , returnStdout: true).trim()
                //         }

                //         sh "kubectl delete namespace $TESTING_NAMESPACE --wait=true || true"
                //         sh 'scripts/ci/install-catalog-source.sh'
                //         sh 'make install'
                //         // Create the Operator image so that it can be used for ConfigListener deployments
                //         sh "make operator-build operator-push IMG=$CONFIG_LISTENER_IMAGE"
                //     }
                // }

                // stage('Infinispan') {
                //     steps {
                //         catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                //             sh "make infinispan-test PARALLEL_COUNT=5"
                //         }
                //     }
                // }

                // stage('Cache') {
                //     steps {
                //         catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                //             sh "make cache-test PARALLEL_COUNT=5"
                //         }
                //     }
                // }

                // stage('Batch') {
                //     steps {
                //         catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                //             sh 'make batch-test PARALLEL_COUNT=5'
                //         }
                //     }
                // }

                // stage('Multinamespace') {
                //     steps {
                //         catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                //             sh "kubectl config use-context $TESTING_CONTEXT"
                //             sh 'make multinamespace-test'
                //         }
                //     }
                // }

                // stage('Backup/Restore') {
                //     steps {
                //         catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                //             sh 'make backuprestore-test INFINISPAN_CPU=500m'
                //         }
                //     }
                // }

                // stage('Webhook') {
                //     steps {
                //         catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                //             sh 'make webhook-test PARALLEL_COUNT=5'
                //         }
                //     }
                // }

                // stage('Hot Rod Rolling Upgrade') {
                //     steps {
                //         catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                //             sh 'make hotrod-upgrade-test'
                //         }
                //     }
                // }

                // stage('Upgrade') {
                //     steps {
                //         catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                //             sh 'make upgrade-test SUBSCRIPTION_STARTING_CSV=infinispan-operator.v2.2.1'
                //         }
                //     }
                // }

                stage('Xsite') {
                    steps {
                        catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                            sh 'scripts/ci/configure-xsite.sh'
                            sh 'make xsite-test'
                        }
                    }

                    post {
                        failure {
                            debugKind(true, 'kind-xsite1', 'kind-xsite2')
                        }
                    }
                }

                stage('Publish test results') {
                    steps {
                        junit testResults: 'test/reports/*.xml', skipPublishingChecks: true
                    }
                }
            }
        }
    }

    post {
        failure {
            debugKind(false, env.TESTING_CONTEXT)
        }

        cleanup {
            sh 'kind delete clusters --all'
            sh 'docker kill $(docker ps -q) || true'
            sh 'docker container prune -f'
            sh 'docker rmi $(docker images -f "dangling=true" -q) || true'
            sh 'docker volume prune -f || true'
            sh 'docker network prune -f || true'
        }
    }
}
