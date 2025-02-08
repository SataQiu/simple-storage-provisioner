docker-build:
	docker build -t shidaqiu/my-storage-provisioner:v0.0.1 .
.PHONY: docker-build

docker-push:
	docker push shidaqiu/my-storage-provisioner:v0.0.1
.PHONY: docker-push
