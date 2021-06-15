# AutoscalingGroup AMI ID update Lambda function

Using Systems Manager - Parameter Store, CloudWatch Event and AWS Lambda function to update AMI ID of Autoscaling Groups.

Target Autoscaling Groups are *Autoscaling Groups which deployed with AWS CodeDeploy - Blue/Green deployment style only.*

![How it works](static/amiupdate.gif)

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

You can deploy below requirement resources using `CFN-AsgAmiUpdater.yaml`

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
      "GoldenAMI"
    ],
    "operation": [
      "Update"
    ]
  }
}
```

2. Create a Parameter in AWS Systems Manager - Parameter Store

Create parameter something like Key : GoldenAMI, Value : ami-12345678

Type : String, Data type : aws:ec2:image

3. Build and Create a Lambda function. Upload built binaries as .zip file.

## !! IMPORTANT

If you just want a test run, set field `DryRun` of `createLaunchTemplateVersionInput` as `true` in `modifyLaunchTemplates` function.

Then it will just rehearse.(Will not create new LaunchTemplate version.)

```bash
[august@dummy-pc ~]$ GOOS=linux go build -o AsgAmiUpdater main.go
[august@dummy-pc ~]$ zip AsgAmiUpdater.zip AsgAmiUpdater
# Upload this .zip file as code while creating Lambda function
```
