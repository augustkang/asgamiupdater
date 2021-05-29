# AutoscalingGroup AMI ID update Lambda function

Using Systems Manager - Parameter Store, CloudWatch Event and AWS Lambda function to update AMI ID of Autoscaling Groups.

Target Autoscaling Groups are *Autoscaling Groups which deployed with AWS CodeDeploy - Blue/Green deployment style only.*


## Prerequisites

1. IAM Role for Lambda : Need below permissions (+ Log Group permissions)

codedeploy:ListApplications

codedeploy:ListDeploymentGroups

codedeploy:GetDeploymentGroup

autoscaling:DescribeAutoscalingGroups

ec2:CreateLaunchTemplateVersion

ec2:DescribeImages

ssm:GetParameter

## Requirements

1. CloudWatch Event to get event for Parameter update event

Event pattern should be as belows:


```
{
  "source": [
    "aws.ssm"
  ],
  "detail-type": [
    "Parameter Store Change"
  ],
  "detail": {
    "name": [
      "test-ami-parameter"
    ],
    "operation": [
      "Update"
    ]
  }
}
```

2. Create a Parameter in AWS Systems Manager - Parameter Store

Create parameter something like Key : My-Ami, Value : ami-12345678

Type : String, Data type : aws:ec2:image

3. Build and Create a Lambda function. Upload built binaries as .zip file.

!! IMPORTANT

실제 변경은 안되게 하고 그냥 테스트삼아 실행하고 싶을 경우

`modifyLaunchTemplates` 함수에서 `tempCreateLaunchTemplateVersionInput` 내에 `DryRun을` true로 설정해주면 됩니다.
이렇게 하면 실제로 런치 템플릿 버전을 생성하진 않고 그냥 생성됐다고만 합니다.(DryRun)

```bash
[august@dummy-pc ~]$ GOOS=linux go build -o AsgAmiUpdater main.go
[august@dummy-pc ~]$ zip AsgAmiUpdater.zip AsgAmiUpdater
# Upload this .zip file as code while creating Lambda function
```

# TODO!!!!!!!!!!
- Improve naming
- Add Comments
- Add detailed logging per each step
- Improve code structure? Not confident..
- CloudFormation Stack
