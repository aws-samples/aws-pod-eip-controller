# AWS Pod EIP Controller

The AWS Pod EIP Controller (PEC) offers a function to automatically allocate and release Elastic IPs via annotations.

## Overview

![Elastic IP controller Overview](/images/Elastic%20IP%20controller%20Overview.png)

The solution processes EIPs for Pods through the following steps:

1. Informers listen for Pod events through List and Watch.
2. The Controller pushes the corresponding Pod keys from the acquired events to the WorkQueue.
3. The Worker gets the Pod key from the WorkQueue and acquires the related Pod information from the Indexer.
4. Based on the Pod's annotation information, the Worker uses the AWS SDK to allocate and associate an EIP for the Pod or disassociate and release the EIP.

## Config

| Flag            | Chart Value          | Type    | Default | Describetion                                                   |
| --------------- | -------------------- | ------- | ------- | -------------------------------------------------------------- |
| N/A             | image                | string  | ''      | aws pod eip controller docker image to deploy                  |
| kubeconfig      | N/A                  | string  | ''      | kubeconfig path, need to provide when debugging locally        |
| vpc-id          | vpcID                | string  | ''      | need to provide when debugging locally or deploying in fargate |
| region          | region               | string  | ''      | need to provide when debugging locally or deploying in fargate |
| watch-namespace | watchNamespace       | string  | ''      | which namespace to listen on only, empty to listen to all      |
| cluster-name    | clusterName          | string  | ''      | eks cluster name                                               |
| log-level       | logLevel             | string  | info    | log level: debug, info, warn, error                            |
| N/A             | createServiceAccount | boolean | false   | whether the helm chart should create service account           |
| resync-period   | resyncPeriod         | int     | 0       | the resync-period for informer                                 |
| N/A             | serviceAccountName   | string  | ''      | The serviceaccount name used by Pod EIP controller             |

## Annotations

| Name                                                           | Type   | Default | Location |
| -------------------------------------------------------------- | ------ | ------- | -------- |
| aws-samples.github.com/aws-pod-eip-controller-type             | string |         | pod      |
| aws-samples.github.com/aws-pod-eip-controller-public-ipv4-pool | string |         | pod      |
| aws-samples.github.com/aws-pod-eip-controller-fixed-tag        | string |         | pod      |
| aws-samples.github.com/aws-pod-eip-controller-fixed-tag-value  | string |         | pod      |

### Automatically apply for EIP: auto

In automatic mode, the Controller will automatically allocate and associate an EIP when a Pod with the **aws-samples.github.com/aws-pod-eip-controller-type: auto** annotation is added, and disassociate and release the EIP when the Pod is deleted.

By default, the EIP is allocate through the **amazon** ipv4-pool. The ipv4-pool for requesting can be specified by annotation: **aws-samples.github.com/aws-pod-eip-controller-public-ipv4-pool**.

#### Example

Add support for binding EIP to Pods

```yaml
aws-samples.github.com/aws-pod-eip-controller-type: auto
```

Allocate EIP from ipv4-pool **amazon-staticbgp**

```yaml
aws-samples.github.com/aws-pod-eip-controller-type: auto
aws-samples.github.com/aws-pod-eip-controller-public-ipv4-pool: amazon-staticbgp
```

### Allocate through EIP's Tag: fixed-tag

In this mode, the Controller allocates EIPs by specifying the EIP's Tag. When deleting a Pod, it will not release the EIP. You need to pre-tag the requested EIPs accordingly.

#### Example

In an environment where an EIP containing the tag-key **pec-ip-pool** has been allocated for but not associated.

```shell
# command
aws ec2 describe-addresses --filters Name=tag-key,Values=pec-ip-pool --query 'Addresses[?AssociationId==null]'
# result
[
    {
        "PublicIp": "123.123.123.123",
        "AllocationId": "eipalloc-0bc8fa6ecc46abcde",
        "Domain": "vpc",
        "Tags": [
            {
                "Key": "pec-ip-pool",
                "Value": ""
            }
        ],
        "PublicIpv4Pool": "amazon",
        "NetworkBorderGroup": "ap-southeast-1"
    }
]
```

Assign the Controller to use the EIP with the tag-key **pec-ip-pool** for Pod processing.

```yaml
aws-samples.github.com/aws-pod-eip-controller-type: fixed-tag
aws-samples.github.com/aws-pod-eip-controller-fixed-tag: pec-ip-pool
```

### Allocate EIP through EIP's Tag and Value: fixed-tag-value

In this mode, the Controller allocates EIPs by specifying the EIP's Tag and Podkey (composed of NameSpace and PodName). When deleting a Pod, the EIP will not be released. It is necessary to pre-set the corresponding TAG and PodKey for the requested EIP.

This mode is more suitable for StatefulSet, and requires processing the EIP tag value through the Pod's Name.

#### Example

For the Pod **app-nginx-demo-0** in the NameSpace **nginx-demo-ns**, allocate for an EIP in advance and set the corresponding tag and value.

```shell
# command
aws ec2 describe-addresses --filters Name=tag:pec-ip-pool,Values=nginx-demo-ns/app-nginx-demo-0
# result
{
    "Addresses": [
        {
            "PublicIp": "123.123.123.123",
            "AllocationId": "eipalloc-0bc8fa6ecc46abcde",
            "Domain": "vpc",
            "Tags": [
                {
                    "Key": "pec-ip-pool",
                    "Value": "nginx-demo-ns/app-nginx-demo-0"
                }
            ],
            "PublicIpv4Pool": "amazon",
            "NetworkBorderGroup": "ap-southeast-1"
        }
    ]
}
```

Specify the Controller to use the EIP with the tag-key **pec-ip-pool** for processing Pods.

```yaml
aws-samples.github.com/aws-pod-eip-controller-type: fixed-tag-value
aws-samples.github.com/aws-pod-eip-controller-fixed-tag-value: pec-ip-pool
```

## Instructions for Use

* If the Pod is deleted in the case where the Controller exits, the Controller will not be able to capture the deletion event, resulting in the inability to perform the correct Pod exit processing.
* In the fixed-tag mode, to avoid the same EIP being contested by multiple Pods, the Controller currently uses a queuing mechanism for processing. In other words, when the fixed-tag is the same as aws-samples.github.com/aws-pod-eip-controller-fixed-tag, queuing will occur, which may result in some delay in large-scale usage.
* The Controller's cleanup of Pod EIP depends on EIP's Tags, avoiding modification of Tags in EIP with the prefix aws-samples.github.com.
* When using the fixed-tag and fixed-tag-value modes, if multiple EKS clusters match the same tag simultaneously, there will be a contention for the EIP.

## Prerequisites

* Install [eksctl](https://docs.aws.amazon.com/eks/latest/userguide/eksctl.html).
* Install [helm](https://helm.sh/docs/intro/install/)
* Install [AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html).
* Install [kubectl](https://kubernetes.io/docs/tasks/tools/).
* install [git](https://github.com/git-guides/install-git).
* install [docker](https://docs.docker.com/engine/install/).
* install [docker buildx](https://docs.docker.com/build/building/multi-platform/).

## Walkthrough

### Create an EKS cluster

Set the current account and region

```shell
export ACCOUNT_ID=$(aws sts get-caller-identity --output text --query Account)
export AWS_REGION=<your-region>
```

**Note**: Replace \<your-region\> with the region where your EKS cluster is located.

This command will create a nodegroup called main at the same time, which contains two instances of type m5.large, and deploy them in the public subnet.

```shell
cat << EOF > eip-demo-cluster.yaml
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig

metadata:
  name: eip-controller-demo
  region: ${AWS_REGION}
  version: "1.34"

iam:
  withOIDC: true
managedNodeGroups:
  - name: main
    instanceType: m6i.large
    desiredCapacity: 2
    privateNetworking: false
EOF
eksctl create cluster -f eip-demo-cluster.yaml
kubectl get nodes
```

![EKS nodes](images/EKS%20nodes.png)

### Build image and push to Amazon Elastic Container Registry

Create ECR repository and login.

```shell
aws ecr create-repository --repository-name aws-pod-eip-controller
aws ecr get-login-password --region ${AWS_REGION} \
    | docker login --username AWS \
    --password-stdin ${ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com
```

Download the sample Pod EIP Controller code, build the image and push it to ECR.

```shell
git clone https://github.com/aws-samples/aws-pod-eip-controller.git
cd aws-pod-eip-controller
docker buildx build \
    --tag ${ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/aws-pod-eip-controller:latest \
    --platform linux/amd64,linux/arm64 \
    --push .
```

### Deploy Pod EIP controller

Create the IAM policy needed for the controller.

```shell
cat << EOF > iam-policy.json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "VisualEditor0",
            "Effect": "Allow",
            "Action": [
                "ec2:AllocateAddress",
                "ec2:AssociateAddress",
                "ec2:CreateTags",
                "ec2:ReleaseAddress",
                "ec2:DisassociateAddress",
                "ec2:DeleteTags",
                "ec2:DescribeAddresses",
                "ec2:DescribeNetworkInterfaces"
            ],
            "Resource": "*"
        }
    ]
}
EOF
aws iam create-policy \
    --policy-name AWSPodEIPControllerIAMPolicy \
    --policy-document file://iam-policy.json
```

Create an IAM role and Kubernetes ServiceAccount for the controller.

```shell
eksctl create iamserviceaccount \
    --cluster=eip-controller-demo \
    --namespace=kube-system \
    --name=aws-pod-eip-controller \
    --attach-policy-arn=arn:aws:iam::${ACCOUNT_ID}:policy/AWSPodEIPControllerIAMPolicy \
    --override-existing-serviceaccounts \
    --region ${AWS_REGION} \
    --approve
```

Deploy aws-pod-eip-controller helm chart

```shell
helm install aws-pod-eip-controller ./charts/aws-pod-eip-controller \
  --namespace kube-system \
  --set image=${ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/aws-pod-eip-controller:latest \
  --set clusterName=eip-controller-demo \
  --set serviceAccountName=aws-pod-eip-controller \
  --wait
```

This command will create the aws-pod-eip-controller deployment in the kube-system namespace.

![aws-pod-eip-controller deployment](images/aws-pod-eip-controller%20deployment.png)

### Usage example

```shell
cat << EOF > nginx.demo.yaml
---
apiVersion: v1
kind: Namespace
metadata:
  name: nginx-demo-ns
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: nginx-user
  namespace: nginx-demo-ns
---
apiVersion: apps/v1
kind: Deployment
metadata:
  namespace: nginx-demo-ns
  name: app-nginx-demo
  labels:
    app.kubernetes.io/name: app-nginx-demo
    version: v1
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app-nginx-demo
  template:
    metadata:
      labels:
        app: app-nginx-demo
      annotations:
        aws-samples.github.com/aws-pod-eip-controller-type: auto
    spec:
      serviceAccountName: nginx-user
      containers:
      - image: nginx:1.20
        name: nginx
        ports:
        - containerPort: 80
          protocol: TCP
        resources:
          limits:
            cpu: "0.5"
            memory: "512Mi"
          requests:
            cpu: "0.1"
            memory: "128Mi"
        volumeMounts:
        - name: podinfo
          mountPath: /etc/podinfo
      initContainers:
      - image: busybox:1.28
        name: innit
        command: ['timeout', '-t' ,'60', 'sh','-c', "until grep -E '^aws-pod-eip-controller-public-ip?' /etc/podinfo/labels; do echo waiting for labels; sleep 2; done"]
        volumeMounts:
        - name: podinfo
          mountPath: /etc/podinfo
      volumes:
        - name: podinfo
          downwardAPI:
            items:
            - path: "labels"
              fieldRef:
                fieldPath: metadata.labels
EOF
kubectl apply -f nginx.demo.yaml
```

Run this command to see the name of the Pod.

```shell
kubectl get pod -n nginx-demo-ns
```

![demo pod](images/demo%20pod.png)

Run this command to see the associated EIP.

```shell
kubectl get pods <your-pod-name> \
    -o=custom-columns=NAME:.metadata.name,STATUS:.status.phase,PODIP:.status.podIP,EIP:.metadata.labels.aws-pod-eip-controller-public-ip \
    -n nginx-demo-ns \
    -w
```

**Note**: You need to replace \<your-pod-name\> with the actual name of your nginx Pod.

![watch pod](images/watch%20pod.png)

**Note**: In the security group where this EIP is located, adding an access rule for port 80 will allow you to access
the Pod through the EIP.

**Note**: Similarly, you can also mount the relevant labels to the file system through the downwardAPI for access. As shown in the example, they are mounted to the /etc/podinfo/labels path.

## Cleanup

To avoid charges, delete your AWS resources.

```shell
kubectl delete -f nginx.demo.yaml
```

**Note**: Deleting the deployment will cause the controller to release the associated EIP.

```shell
eksctl delete cluster -f eip-demo-cluster.yaml
aws iam delete-policy \
    --policy-arn arn:aws:iam::${ACCOUNT_ID}:policy/AWSPodEIPControllerIAMPolicy
aws ecr delete-repository --repository-name aws-pod-eip-controller --force
```

Note: Execute the command in the folder where eip-demo-cluster.yaml is located.

## Conclusion

In this post, you deployed the EIP controller in an EKS cluster. By listening to Pod creation and deletion events,
it associates and disassociates EIP for Pods annotated with specific annotations. This simplifies application access.
Pods can be directly accessed via EIP without needing additional Load Balancers or Ingress Controllers. It enables
automated operations. The annotations and controller approach fully automates the EIP allocation and release process
without requiring human intervention.
