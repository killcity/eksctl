package cloudformation

import (
	"encoding/json"
)

// AWSServerlessFunction_S3NotificationFilter AWS CloudFormation Resource (AWS::Serverless::Function.S3NotificationFilter)
// See: http://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-properties-s3-bucket-notificationconfiguration-config-filter.html
type AWSServerlessFunction_S3NotificationFilter struct {

	// S3Key AWS CloudFormation Property
	// Required: true
	// See: http://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-properties-s3-bucket-notificationconfiguration-config-filter.html
	S3Key *Value `json:"S3Key,omitempty"`
}

// AWSCloudFormationType returns the AWS CloudFormation resource type
func (r *AWSServerlessFunction_S3NotificationFilter) AWSCloudFormationType() string {
	return "AWS::Serverless::Function.S3NotificationFilter"
}

func (r *AWSServerlessFunction_S3NotificationFilter) MarshalJSON() ([]byte, error) {
	return json.Marshal(*r)
}
