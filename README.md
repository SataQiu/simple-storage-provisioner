# The simplest implementation of dynamic volume provisioning

- Implement the `Provision` interface to provide dynamic hostPath volume
- Implement the `Delete` interface to clean up dynamic hostPath volume

## Deploy the storage plugin

```
kubectl apply -f manifests.yaml
```

## Enjoy it

### WaitForFirstConsumer

```
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: example-pvc
spec:
  storageClassName: my-storage
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 128Mi
---
apiVersion: v1
kind: Pod
metadata:
  name: example
spec:
  containers:
  - name: example
    image: nginx:stable-alpine
    imagePullPolicy: IfNotPresent
    volumeMounts:
    - name: volv
      mountPath: /data
    ports:
    - containerPort: 80
  volumes:
  - name: volv
    persistentVolumeClaim:
      claimName: example-pvc
EOF
```

### Immediate

```
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: example-pvc-fast
spec:
  storageClassName: my-storage-fast
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 128Mi
EOF
```

```sh
kubectl get pvc

NAME               STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS      AGE
example-pvc        Bound    pvc-45a42b7b-06f5-478f-b3e0-b3e0d476a19d   128Mi      RWO            my-storage        3m58s
example-pvc-fast   Bound    pvc-7a921975-ec84-43a0-b8ca-3f153541202b   128Mi      RWO            my-storage-fast   61s
```
