pipeline {
  agent any
  stages {
    stage('Format') { steps { sh 'test -z "$(gofmt -l .)"' } }
    stage('Test') { steps { sh 'go test ./...' } }
    stage('Vet') { steps { sh 'go vet ./...' } }
    stage('Build') { steps { sh 'go build ./...' } }
  }
}
