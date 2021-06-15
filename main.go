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
			fmt.Printf("[ERROR] Failed to get paginator.NextPage output in getApplicationNames")
			log.Println(err.Error())
			os.Exit(0)
		}
		applicationNames = append(applicationNames, output.Applications...)
	}

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
					fmt.Printf("[INFO] This deployment group has no AutoscalinGroups\n")
					fmt.Printf("[INFO] Application Name : %s, DeploymentGroup name : %s\n", applicationName, deploymentGroupName)
					fmt.Printf("[INFO] Skipping this DeploymentGroup\n")
					continue
				} else {
					var asgNameList []string
					asgNameList = append(asgNameList, *deploymentGroupOutput.DeploymentGroupInfo.AutoScalingGroups[0].Name)
					asgClient := autoscaling.NewFromConfig(cfg)
					output, err := asgClient.DescribeAutoScalingGroups(context.TODO(), &autoscaling.DescribeAutoScalingGroupsInput{
						AutoScalingGroupNames: asgNameList,
					})
					if err != nil {
						log.Printf("[ERROR] DescribeAutoscalingGroups failed!! - ASG : %s, App : %s, DeploymentGroup : %s\n\n", asgNameList[0], applicationName, *deploymentGroupOutput.DeploymentGroupInfo.DeploymentGroupName)
						fmt.Println(err.Error())
						continue
					}
					autoscalingGroups = append(autoscalingGroups, output.AutoScalingGroups...)
				}
			}
		}
	}
	if len(autoscalingGroups) == 0 {
		fmt.Printf("[ERROR] There's no target ASGs. Exit function\n")
		os.Exit(0)
	}
	fmt.Printf("[INFO] Target AutoscalingGroup names as belows : \n")
	for _, autoscalingGroup := range autoscalingGroups {
		fmt.Printf("%s ", *autoscalingGroup.AutoScalingGroupName)
	}
	return autoscalingGroups
}

// modifyLaunchTemplates updates launch template version of Autoscaling Groups to use new AMI
func modifyLaunchTemplates(cfg aws.Config, autoscalingGroups []asgTypes.AutoScalingGroup, imageId *string) {

	client := ec2.NewFromConfig(cfg)
	updated := make(map[string]bool)

	// get snapshot id of ami
	describeImagesOutput, err := client.DescribeImages(context.TODO(), &ec2.DescribeImagesInput{
		DryRun:   aws.Bool(false),
		ImageIds: []string{*imageId},
	})
	if err != nil {
		fmt.Printf("[FAIL] describe image failed! Exit Lambda\n\n")
		fmt.Println(err.Error())
		os.Exit(0)
	}

	snapshotId := describeImagesOutput.Images[0].BlockDeviceMappings[0].Ebs.SnapshotId

	for _, autoscalingGroup := range autoscalingGroups {
		launchTemplateId := autoscalingGroup.LaunchTemplate.LaunchTemplateId
		currentVersionLaunchTemplate, err := client.DescribeLaunchTemplateVersions(context.TODO(), &ec2.DescribeLaunchTemplateVersionsInput{
			DryRun:           aws.Bool(false),
			LaunchTemplateId: launchTemplateId,
			Versions:         []string{"$Latest"},
		})
		if err != nil {
			fmt.Printf("[ERROR] Failed to get current version launchtemplate : %s \n\nSkipping this launchtemplate\n", *launchTemplateId)
			fmt.Println(err.Error())
			continue
		}

		blockDeviceMappingRequest := ec2Types.LaunchTemplateBlockDeviceMappingRequest{
			DeviceName: currentVersionLaunchTemplate.LaunchTemplateVersions[0].LaunchTemplateData.BlockDeviceMappings[0].DeviceName,
			Ebs: &ec2Types.LaunchTemplateEbsBlockDeviceRequest{
				DeleteOnTermination: currentVersionLaunchTemplate.LaunchTemplateVersions[0].LaunchTemplateData.BlockDeviceMappings[0].Ebs.DeleteOnTermination,
				Encrypted:           currentVersionLaunchTemplate.LaunchTemplateVersions[0].LaunchTemplateData.BlockDeviceMappings[0].Ebs.Encrypted,
				Iops:                currentVersionLaunchTemplate.LaunchTemplateVersions[0].LaunchTemplateData.BlockDeviceMappings[0].Ebs.Iops,
				SnapshotId:          snapshotId,
				Throughput:          currentVersionLaunchTemplate.LaunchTemplateVersions[0].LaunchTemplateData.BlockDeviceMappings[0].Ebs.Throughput,
				VolumeSize:          currentVersionLaunchTemplate.LaunchTemplateVersions[0].LaunchTemplateData.BlockDeviceMappings[0].Ebs.VolumeSize,
				VolumeType:          currentVersionLaunchTemplate.LaunchTemplateVersions[0].LaunchTemplateData.BlockDeviceMappings[0].Ebs.VolumeType,
			},
		}

		createLaunchTemplateVersionInput := ec2.CreateLaunchTemplateVersionInput{
			LaunchTemplateData: &ec2Types.RequestLaunchTemplateData{
				ImageId:             aws.String(*imageId),
				BlockDeviceMappings: []ec2Types.LaunchTemplateBlockDeviceMappingRequest{blockDeviceMappingRequest},
			},
			DryRun:           aws.Bool(true), // if set this false, it will create launchtemplateversion
			LaunchTemplateId: launchTemplateId,
			SourceVersion:    aws.String("$Latest"),
		}

		// Some ASGs use same launchtemplate. If they both create launch template version, that launch template will experience redundant version update
		// In order to avoid duplicated updates, check whether that launchtemplate has already updated or not
		if _, present := updated[*launchTemplateId]; !present {
			_, err := client.CreateLaunchTemplateVersion(context.TODO(), &createLaunchTemplateVersionInput)
			if err != nil {
				fmt.Printf("[FAIL] ASG : %s launch template not updated\n\n", *autoscalingGroup.AutoScalingGroupName)
				log.Println(err.Error())
			} else {
				updated[*launchTemplateId] = true
				fmt.Printf("[SUCCESS] ASG : %s - launch template updated\n\n", *autoscalingGroup.AutoScalingGroupName)
				fmt.Printf("[SUCCESS] Updated LaunchTemplate ID : %s\n\n", *launchTemplateId)
			}
		} else {
			fmt.Printf("[INFO] Skipping ASG %s launchtemplate update - updated before update : %s", *autoscalingGroup.AutoScalingGroupName, *launchTemplateId)
		}
	}

}

// getImageIdFromParameterKey gets AMI id value from Systems manager - Parameter store
func getImageIdFromParameterKey(cfg aws.Config, parameterKey string) (imageId *string) {
	client := ssm.NewFromConfig(cfg)
	getParameterOutput, err := client.GetParameter(context.TODO(), &ssm.GetParameterInput{
		Name: aws.String(parameterKey),
	})
	if err != nil {
		fmt.Printf("[ERROR] Failed to getParameter from Parameter Store. Exit function.")
		log.Println(err.Error())
		os.Exit(0)
	}
	return getParameterOutput.Parameter.Value

}

// Event pattern
// {
//     "dataType": "aws:ec2:image",
//     "name": "GoldenAMI",
//     "description": "Golden AMI Parameter Update Event",
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
	fmt.Println("[INFO] Updated parameter name is as belows")

	err := json.Unmarshal(event.Detail, &eventDetail)
	if err != nil {
		fmt.Println("[ERROR] Failed to unmarshal Event Detail")
		log.Println(err.Error())
		os.Exit(0)
	}

	if eventDetail.DataType != "aws:ec2:image" {
		fmt.Println("[ERROR] This event is not created by aws:ec2:image type Parameter")
		os.Exit(0)
	}

	fmt.Println(eventDetail.Name)

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		fmt.Println("[ERROR] Config loading failed")
		log.Println(err.Error())
		os.Exit(0)
	}

	var applicationNames []string = getApplicationNames(cfg)

	autoScalingGroups := getAutoScalingGroups(cfg, applicationNames)

	imageId := getImageIdFromParameterKey(cfg, eventDetail.Name)
	modifyLaunchTemplates(cfg, autoScalingGroups, imageId)

}

func main() {
	lambda.Start(handleRequest)
}
