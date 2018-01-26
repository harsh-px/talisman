export KUBECONFIG=<path-to=kubeconfig>
export PX_KVDB_ENDPOINTS=etcd:http://192.168.56.70:2379

ginkgo -v
