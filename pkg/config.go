package pkg

const (
	// Kubernetes annotations
	PodEIPAnnotationKey                = "aws-samples.github.com/aws-pod-eip-controller-type"
	PodEIPAnnotationValueAuto          = "auto"
	PodEIPAnnotationValueFixedTag      = "fixed-tag"
	PodEIPAnnotationValueFixedTagValue = "fixed-tag-value"

	PodAddressPoolAnnotationKey          = "aws-samples.github.com/aws-pod-eip-controller-public-ipv4-pool"
	PodAddressFixedTagAnnotationKey      = "aws-samples.github.com/aws-pod-eip-controller-fixed-tag"
	PodAddressFixedTagValueAnnotationKey = "aws-samples.github.com/aws-pod-eip-controller-fixed-tag-value"

	// Kubernetes labels
	PodPublicIPLabel         = "aws-pod-eip-controller-public-ip"
	PodEIPAnnotationKeyLabel = "aws-pod-eip-controller-type"
	PodAddressPoolIDLabel    = "aws-pod-eip-controller-public-ipv4-pool"
	PodFixedTagLabel         = "aws-pod-eip-controller-fixed-tag"
	PodFixedTagValueLabel    = "aws-pod-eip-controller-fixed-tag-value"

	// AWS Tags
	TagTypeKey        = "aws-samples.github.com/aws-pod-eip-controller-type"
	TagClusterNameKey = "aws-samples.github.com/aws-pod-eip-controller-cluster-name"
	TagPodKey         = "aws-samples.github.com/aws-pod-eip-controller-pod"
)

func ValidPECType(pecType string) bool {
	return pecType == PodEIPAnnotationValueAuto || pecType == PodEIPAnnotationValueFixedTag || pecType == PodEIPAnnotationValueFixedTagValue
}
