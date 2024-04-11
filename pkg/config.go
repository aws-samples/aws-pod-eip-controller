package pkg

const (
	// Kubernetes annotations/labels

	PodEIPAnnotationKey         = "aws-samples.github.com/aws-pod-eip-controller-type"
	PodEIPAnnotationValue       = "auto"
	PodEIPReclaimAnnotationKey  = "aws-samples.github.com/aws-pod-eip-controller-reclaim"
	PodEIPReclaimAnnotationVal  = "false"
	PodEIPModeAnnotationKey     = "aws-samples.github.com/aws-pod-eip-controller-mode"
	PodEIPModeAnnotationVal     = "fixed"
	PodAddressPoolAnnotationKey = "aws-samples.github.com/aws-pod-eip-controller-public-ipv4-pool"
	PodPublicIPLabel            = "aws-pod-eip-controller-public-ip"

	// AWS Tags

	TagTypeKey        = "aws-samples.github.com/aws-pod-eip-controller-type"
	TagClusterNameKey = "aws-samples.github.com/aws-pod-eip-controller-cluster-name"
	TagPodKey         = "aws-samples.github.com/aws-pod-eip-controller-pod"
)
