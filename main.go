package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	asgTypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/aws/aws-sdk-go-v2/service/codedeploy"
	codedeployTypes "github.com/aws/aws-sdk-go-v2/service/codedeploy/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

type EventDetail struct {
	DataType    string
	Name        string
	Description string
	Type        string
	Operation   string
}

// getApplicationNames gets application names from CodeDeploy
func getApplicationNames(cfg aws.Config) (appNames []string) {
	var applicationNames []string

	client := codedeploy.NewFromConfig(cfg)
	listApplicationsInput := codedeploy.ListApplicationsInput{}
	paginator := codedeploy.NewListApplicationsPaginator(client, &listApplicationsInput)
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(context.TODO())
		if err != nil {
			log.Println(err.Error())
			os.Exit(0)
		}
		applicationNames = append(applicationNames, output.Applications...)
	}

	fmt.Printf("Gathered CodeDeploy application names as :\n%v", applicationNames)

	return applicationNames
}

// getAutoScalingGroups gets autoscalinggroup information
func getAutoScalingGroups(cfg aws.Config, applicationNames []string) (asgs []asgTypes.AutoScalingGroup) {

	codeDeployClient := codedeploy.NewFromConfig(cfg)

	var autoscalingGroups []asgTypes.AutoScalingGroup

	for _, applicationName := range applicationNames {
		output, err := codeDeployClient.ListDeploymentGroups(context.TODO(), &codedeploy.ListDeploymentGroupsInput{
			ApplicationName: &applicationName,
		})
		if err != nil {
			log.Println(err.Error())
			os.Exit(0)
		}

		for _, deploymentGroupName := range output.DeploymentGroups {
			deploymentGroupOutput, err := codeDeployClient.GetDeploymentGroup(context.TODO(), &codedeploy.GetDeploymentGroupInput{
				ApplicationName:     &applicationName,
				DeploymentGroupName: &deploymentGroupName,
			})
			if err != nil {
				log.Println(err.Error())
				os.Exit(0)
			}
			if deploymentGroupOutput.DeploymentGroupInfo.ComputePlatform == codedeployTypes.ComputePlatformServer && deploymentGroupOutput.DeploymentGroupInfo.DeploymentStyle.DeploymentType == codedeployTypes.DeploymentTypeBlueGreen {
				if len(deploymentGroupOutput.DeploymentGroupInfo.AutoScalingGroups) == 0 {
					fmt.Printf("This deployment group has no AutoscalinGroups\n")
					fmt.Printf("Application Name : %s, DeploymentGroup name : %s\n", applicationName, deploymentGroupName)
					fmt.Printf("Skipping this DeploymentGroup\n")
					continue
				} else {
					var asgNameList []string
					asgNameList = append(asgNameList, *deploymentGroupOutput.DeploymentGroupInfo.AutoScalingGroups[0].Name)
					asgClient := autoscaling.NewFromConfig(cfg)
					output, err := asgClient.DescribeAutoScalingGroups(context.TODO(), &autoscaling.DescribeAutoScalingGroupsInput{
						AutoScalingGroupNames: asgNameList,
					})
					if err != nil {
						log.Println("Getting AutoscalingGroup Output gone wrong. Close function")
						os.Exit(0)
					}
					autoscalingGroups = append(autoscalingGroups, output.AutoScalingGroups...)
				}
			}
		}
	}
	if len(autoscalingGroups) == 0 {
		fmt.Printf("There's no target ASGs. Exit function\n")
		os.Exit(0)
	}
	fmt.Printf("Target AutoscalingGroup names as belows : \n")
	for _, autoscalingGroup := range autoscalingGroups {
		fmt.Printf("%s ", *autoscalingGroup.AutoScalingGroupName)
	}
	return autoscalingGroups
}

// modifyLaunchTemplates updates launch template version of Autoscaling Groups to use new AMI
func modifyLaunchTemplates(cfg aws.Config, autoscalingGroups []asgTypes.AutoScalingGroup, imageId *string) bool {
	var result bool = false
	client := ec2.NewFromConfig(cfg)
	for _, autoscalingGroup := range autoscalingGroups {
		launchTemplateId := autoscalingGroup.LaunchTemplate.LaunchTemplateId
		createLaunchTemplateVersionInput := ec2.CreateLaunchTemplateVersionInput{
			LaunchTemplateData: &ec2Types.RequestLaunchTemplateData{
				ImageId: aws.String(*imageId),
			},
			DryRun:           aws.Bool(true), // if set this false, it will create launchtemplateversion
			LaunchTemplateId: launchTemplateId,
			SourceVersion:    aws.String("$Latest"),
		}
		_, err := client.CreateLaunchTemplateVersion(context.TODO(), &createLaunchTemplateVersionInput)
		if err != nil {
			fmt.Printf("[FAIL] ASG : %s launch template not updated\n", *autoscalingGroup.AutoScalingGroupName)
			log.Println(err.Error())
			os.Exit(0)
		} else {
			fmt.Printf("[SUCCESS] ASG : %s launch template updated\n", *autoscalingGroup.AutoScalingGroupName)
		}
	}
	result = true
	return result
}

// getImageIdFromParameterKey gets AMI id value from Systems manager - Parameter store
func getImageIdFromParameterKey(cfg aws.Config, parameterKey string) (imageId *string) {
	client := ssm.NewFromConfig(cfg)
	getParameterOutput, err := client.GetParameter(context.TODO(), &ssm.GetParameterInput{
		Name: aws.String(parameterKey),
	})
	if err != nil {
		log.Println(err.Error())
	}
	return getParameterOutput.Parameter.Value

}

// Event pattern
// {
//     "dataType": "aws:ec2:image",
//     "name": "test-ami-parameter",
//     "description": "parameter store test ami",
//     "type": "String",
//     "operation": "Update"
// }

func handleRequest(ctx context.Context, event events.CloudWatchEvent) {
	fmt.Printf("Event Version : %s\n", event.Version)
	fmt.Printf("Event ID : %s\n", event.ID)
	fmt.Printf("Event DetailType : %s\n", event.DetailType)
	fmt.Printf("Event Source : %s\n", event.Source)
	fmt.Printf("Event Account ID : %s\n", event.AccountID)
	fmt.Printf("Event Resources : %v\n", event.Resources)

	eventDetail := EventDetail{}
	fmt.Println("Parameter name is as belows")

	err := json.Unmarshal(event.Detail, &eventDetail)
	if err != nil {
		fmt.Println("Failed to unmarshal Event Detail")
		os.Exit(0)
	}

	if eventDetail.DataType != "aws:ec2:image" {
		fmt.Println("FATAL : This event is not created by aws:ec2:image type Parameter")
		os.Exit(0)
	}

	fmt.Println(eventDetail.Name)

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		fmt.Println("Config loading failed")
		os.Exit(0)
	}

	var applicationNames []string = getApplicationNames(cfg)

	autoScalingGroups := getAutoScalingGroups(cfg, applicationNames)

	imageId := getImageIdFromParameterKey(cfg, eventDetail.Name)
	result := modifyLaunchTemplates(cfg, autoScalingGroups, imageId)
	if result {
		fmt.Println("Launchtemplate ImageId Successfully updated")
	} else {
		fmt.Println("Imageid not updated")
	}

}

func main() {
	lambda.Start(handleRequest)
}
