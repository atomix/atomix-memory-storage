module github.com/atomix/cache-storage-controller

go 1.13

require (
	cloud.google.com/go v0.43.0 // indirect
	github.com/atomix/api v0.0.0-20200211005812-591fe8b07ea8
	github.com/atomix/kubernetes-controller v0.2.0-beta.4
	github.com/gogo/protobuf v1.3.1
	github.com/golang/protobuf v1.3.2
	github.com/hashicorp/golang-lru v0.5.3 // indirect
	go.uber.org/atomic v1.4.0 // indirect
	golang.org/x/net v0.0.0-20191112182307-2180aed22343 // indirect
	golang.org/x/sys v0.0.0-20191113165036-4c7a9d0fe056 // indirect
	k8s.io/api v0.17.2
	k8s.io/apimachinery v0.17.2
	k8s.io/client-go v0.17.2
	sigs.k8s.io/controller-runtime v0.5.2
)

replace github.com/atomix/kubernetes-controller => ../atomix-k8s-controller
