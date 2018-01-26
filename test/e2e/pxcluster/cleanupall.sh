#!/bin/bash

kubectl get all --all-namespaces  -l name=portworx  | grep -v NAME | awk '{print $2}' | xargs kubectl delete -n kube-system

kubectl delete clusterrole portworx
kubectl delete  serviceaccount portworx -n kube-system
kubectl delete  clusterrolebinding  portworx -n kube-system
