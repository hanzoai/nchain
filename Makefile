IMG ?= ghcr.io/hanzoai/nchain:latest

.PHONY: build test vet fmt manifests docker-build docker-push deploy

build:
	go build -o bin/manager cmd/main.go

test:
	go test ./... -v

vet:
	go vet ./...

fmt:
	go fmt ./...

manifests:
	controller-gen crd rbac:roleName=nchain-operator paths="./api/..." output:crd:artifacts:config=config/crd/bases

docker-build:
	docker buildx build --platform linux/amd64 -t $(IMG) .

docker-push:
	docker buildx build --platform linux/amd64 --push -t $(IMG) .

deploy: manifests
	kubectl apply -k config/default
