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
        TESTING_LOG_DIR = "$WORKSPACE/log"
        WATCH_NAMESPACE = 'namespace-for-testing'
        PATH="/opt/go/bin:$PATH"
        RUN_SA_OPERATOR = 'true'
        MAKE_DATADIR_WRITABLE = 'true'
        CONFIG_LISTENER_IMAGE = 'localhost:5001/infinispan-operator'
        SERVER_TAGS = '13.0.10.Final 14.0.1.Final 14.0.6.Final 14.0.9.Final 14.0.13.Final 14.0.17.Final'
        TEST_REPORT_DIR = "$WORKSPACE/test/reports"
        CHANGE_TARGET = "${env.CHANGE_TARGET}"
        THREAD_DUMP_PRE_STOP = 'true'
    }

    options {
        timeout(time: 120, unit: 'MINUTES')
        timestamps()
        buildDiscarder(logRotator(numToKeepStr: '100', daysToKeepStr: '61'))
    }

    stages {
        stage('Execute') {
            when {
                anyOf {
                    changeset "go.*"
                    changeset "**/*.go"
                    changeset "pkg/**/*"
                    changeset "test/**/*"
                    changeset "Dockerfile"
                    changeset "scripts/**/*"
                    changeset "config/**/*"
                    changeset "Jenkinsfile"

                    expression {
                        // Always build non PRS
                        return env.CHANGE_TARGET == "null"
                    }

                    expression {
                        return sh(script: 'git fetch origin $CHANGE_TARGET && git diff --name-only FETCH_HEAD | grep -qvE \'(\\.md$)|(^(documentation|test-integration|.gitignore))/\'', returnStatus: true) == 0
                    }
                }
            }
            stages {
                stage('Build') {
                    steps {
                        sh 'go mod vendor'
                        sh 'make lint'
                    }
                }

                stage('Unit Test') {
                    steps {
                        sh 'make test'
                    }
                }

                stage('E2E') {
                    stages {
                        stage('Prepare') {
                            steps{
                                sh 'kind delete clusters --all'
                                sh 'cleanup.sh'
                                // Ensure that we always have the latest version of the server images locally
                                sh "for tag in ${SERVER_TAGS}; do docker pull \"quay.io/infinispan/server:\${tag}\"; done"
                                sh 'make go-junit-report'
                            }
                        }

                        stage('Create k8s Cluster') {
                            steps {
                                sh 'scripts/ci/kind-with-olm.sh'
                                writeFile file: "${KUBECONFIG}", text: sh(script: 'kind get kubeconfig', , returnStdout: true)

                                script {
                                    env.TESTING_CONTEXT = sh(script: 'kubectl --insecure-skip-tls-verify config current-context', , returnStdout: true).trim()
                                }

                                sh "kubectl delete namespace $TESTING_NAMESPACE --wait=true || true"
                                sh 'scripts/ci/install-catalog-source.sh'
                                sh 'make install'
                                // Create the Operator image so that it can be used for ConfigListener deployments
                                sh "make operator-build operator-push IMG=$CONFIG_LISTENER_IMAGE"
                            }
                        }

                        stage('Infinispan') {
                            steps {
                                catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                                    sh "make infinispan-test PARALLEL_COUNT=5"
                                }
                            }
                        }

                        stage('Cache') {
                            steps {
                                catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                                    sh "make cache-test PARALLEL_COUNT=5"
                                }
                            }
                        }

                        stage('Batch') {
                            steps {
                                catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                                    sh 'make batch-test PARALLEL_COUNT=5'
                                }
                            }
                        }

                        stage('Multinamespace') {
                            steps {
                                catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                                    sh "kubectl config use-context $TESTING_CONTEXT"
                                    sh 'make multinamespace-test'
                                }
                            }
                        }

                        stage('Backup/Restore') {
                            steps {
                                catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                                    sh 'make backuprestore-test'
                                }
                            }
                        }

                        stage('Webhook') {
                            steps {
                                catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                                    sh 'make webhook-test PARALLEL_COUNT=5'
                                }
                            }
                        }

                        stage('Hot Rod Rolling Upgrade') {
                            steps {
                                catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                                    sh 'make hotrod-upgrade-test SUBSCRIPTION_CHANNEL_SOURCE=2.2.x SUBSCRIPTION_STARTING_CSV=infinispan-operator.v2.2.5'
                                }
                            }
                        }

                        stage('Upgrade') {
                            steps {
                                catchError (buildResult: 'FAILURE', stageResult: 'FAILURE') {
                                    sh 'make upgrade-test SUBSCRIPTION_CHANNEL_SOURCE=2.2.x SUBSCRIPTION_STARTING_CSV=infinispan-operator.v2.2.5 INFINISPAN_CPU=1.0'
                                }
                            }
                        }

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
        }
    }

    post {
        failure {
            debugKind(false, env.TESTING_CONTEXT)
        }

        cleanup {
            archiveArtifacts artifacts: 'log/**/*.yaml,log/**/*.log', followSymlinks: false, allowEmptyArchive: true

            sh 'kind delete clusters --all'
            sh 'docker kill $(docker ps -q) || true'
            sh 'docker container prune -f'
            sh 'docker rmi $(docker images -f "dangling=true" -q) || true'
            sh 'docker volume prune -f || true'
            sh 'docker network prune -f || true'
        }
    }
}
