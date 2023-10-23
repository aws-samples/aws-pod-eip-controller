# AWS Pod EIP Controller

The AWS Pod EIP Controller (PEC) offers a function to automatically allocate and release Elastic IPs via annotations.
It also enables automatic association of allocated EIPs with the PODs and provides the ability to add EIPs to Shied
protection via annotations. This feature enhances security by allowing for better control over IP addresses used in AWS
resources.

## Overview

![Elastic IP controller Overview](/images/Elastic%20IP%20controller%20Overview.png)

The solution processes EIP and Shield for Pods through the following steps:

1. Informers listen for Pod events through List and Watch, and push them to the DeltaFIFO
2. DeltaFIFO sends the acquired events to the WorkQueue  
3. The Processor handles the events; based on the annotation information in the Pod events, it uses the AWS SDK
   to add/remove EIP and join/leave the Shield resource protection for the Pod

## Annotations

| Name                                                 | Type   | Default  | Location |
|------------------------------------------------------|--------|----------|----------|
| aws-samples.github.com/aws-pod-eip-controller-type   | string | auto     | pod      |
| aws-samples.github.com/aws-pod-eip-controller-shield | string | advanced | pod      |

## Config

| Flag            | Chart Value          | Type    | Default | Describetion                                                   |
|-----------------|----------------------|---------|---------|----------------------------------------------------------------|
| N/A             | image                | string  | ''      | aws pod eip controller docker image to deploy                  |
| kubeconfig      | N/A                  | string  | ''      | kubeconfig path, need to provide when debugging locally        |
| vpc-id          | vpcID                | string  | ''      | need to provide when debugging locally or deploying in fargate |
| region          | region               | string  | ''      | need to provide when debugging locally or deploying in fargate |
| watch-namespace | watchNamespace       | string  | ''      | which namespace to listen on only, empty to listen to all      |
| cluster-name    | clusterName          | string  | ''      | eks cluster name                                               |
| log-level       | logLevel             | string  | info    | log level: debug, info, warn, error                            |
| N/A             | createServiceAccount | boolean | false   | whether the helm chart should create service account           |

## Prerequisites

* Install [eksctl](https://docs.aws.amazon.com/eks/latest/userguide/eksctl.html).
* Install [AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html).
* Install [kubectl](https://kubernetes.io/docs/tasks/tools/).
* install [git](https://github.com/git-guides/install-git).
* install [docker](https://docs.docker.com/engine/install/).
* install [docker buildx](https://docs.docker.com/build/architecture/#install-buildx).

## Walkthrough

### Create an EKS cluster using EC2 instances that are deployed in a public subnet.

Set the current account and region

```shell
export ACCOUNT_ID=$(aws sts get-caller-identity --output text --query Account)
export AWS_REGION=<your-region>
```

**Note**: Replace the region where your EKS cluster is deployed.

This command will concurrently create a node group called main. The node group will have instances of type m5.large
and will be deployed in the public subnet.

```shell
cat << EOF > eip-demo-cluster.yaml
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig

metadata:
  name: eip-controller-demo
  region: ${AWS_REGION}
  version: "1.27"

iam:
  withOIDC: true
managedNodeGroups:
  - name: main
    instanceType: m5.large
    desiredCapacity: 2
    privateNetworking: false
EOF
eksctl create cluster -f eip-demo-cluster.yaml
kubectl get nodes
```

![EKS nodes](images/EKS%20nodes.png)

### Build Pod EIP controller image and push to Amazon Elastic Container Registry

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
                "ec2:ReleaseAddress",
                "ec2:DisassociateAddress",
                "ec2:DescribeAddresses",
                "ec2:DescribeNetworkInterfaces",
                "ec2:CreateTags",
                "ec2:AssociateAddress",
                "ec2:AllocateAddress",
                "shield:DeleteProtection",
                "shield:DescribeProtection",
                "shield:DescribeSubscription",
                "shield:CreateProtection"
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
helm install controller ./charts/aws-pod-eip-controller \
  --namespace kube-system \
  --set image=${ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/aws-pod-eip-controller:latest \
  --wait
```

**Note**: The implementation of the example can be found in [aws-samples/aws-pod-eip-controller](https://github.com/aws-samples/aws-pod-eip-controller) repo.

 This command will create the aws-pod-eip-controller deployment in the kube-system namespace.

![aws-pod-eip-controller deployment](images/aws-pod-eip-controller%20deployment.png)

### Deploy the sample deployment

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
        aws-samples.github.com/aws-pod-eip-controller-shield: advanced
    spec:
      serviceAccountName: nginx-user
      containers:
      - image: nginx:1.20
        imagePullPolicy: Always
        name: nginx
        ports:
        - containerPort: 80
          protocol: TCP
EOF
kubectl apply -f nginx.demo.yaml
```

**Note**: In the deployment, two annotations were added to the metadata of the template.

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

**Note**: Replace the pod name.

![watch pod](images/watch%20pod.png)

**Note**: In the security group where this EIP is located, adding an access rule for port 80 will allow you to access
the Pod through the EIP.

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

## Conclusion

In this post, you deployed the EIP controller in an EKS cluster. By listening to Pod creation and deletion events,
it associates and disassociates EIP for Pods annotated with specific annotations. This simplifies application access.
Pods can be directly accessed via EIP without needing additional Load Balancers or Ingress Controllers. It enables
automated operations. The annotations and controller approach fully automates the EIP allocation and release process
without requiring human intervention. It also improves security. The EIP can be directly added to AWS Shield Advanced
for DDoS protection.
