package ecs

import (
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/go-errors/errors"
	"github.com/sirupsen/logrus"
)

var ecsService *ecs.ECS
var autoscalingService *autoscaling.AutoScaling
var ec2Service *ec2.EC2

type ServiceEvent struct {
	CreatedAt *time.Time
	Message   *string
	Id        *string
}

type Service struct {
	ServiceArn       *string
	DesiredTaskCount *int64
	CurrentTaskCount *int64
	PendingTaskCount *int64
	Events           []*ServiceEvent
}

type Task struct {
	TaskArn              *string
	ContainerInstanceArn *string
	Status               *string
	DesiredStatus        *string
	CPU                  *int
	Memory               *int
}

type ContainerInstance struct {
	ContainerInstanceArn *string
	RegisteredDate       *time.Time
	EC2InstanceId        *string
	AgentConnected       *bool
	Status               *string
	RemainingCPU         *int64
	TotalCPU             *int64
	RemainingMemory      *int64
	TotalMemory          *int64
	PendingTasksCount    *int64
	RunningTasksCount    *int64
	AvailabilityZone     *string
}

type ClusterDetails struct {
	ClusterArn           *string
	ContainerInstances   []*ContainerInstance
	Tasks                []*Task
	Services             []*Service
	AutoScalingGroup     *AutoScalingGroupDetails
	TotalMemory          int64
	TotalCPU             int64
	TotalRemainingMemory int64
	TotalRemainingCPU    int64
	TotalRunningTasks    *int64
	TotalPendingTasks    *int64
}

type AutoScalingGroupDetails struct {
	Name                 *string
	AutoScalingGroupArn  *string
	MinInstanceCount     *int64
	MaxInstanceCount     *int64
	DesiredInstanceCount *int64
}

//Initialize the ecs service
func Initialize() {
	// Load session from shared config
	sessionOptions := session.Options{
		Config:            aws.Config{Region: aws.String("us-west-2")},
		SharedConfigState: session.SharedConfigEnable,
	}
	sess := session.Must(session.NewSessionWithOptions(sessionOptions))

	// Create service client value configured for credentials
	// from assumed role.
	ecsService = ecs.New(sess)
	autoscalingService = autoscaling.New(sess)
	ec2Service = ec2.New(sess)
}

func getResourceValue(attributes []*ecs.Resource, attributeName string) *int64 {
	for _, attribute := range attributes {
		if *attribute.Name == attributeName {
			return attribute.IntegerValue
		}
	}
	return nil
}

func getAttributeValue(attributes []*ecs.Attribute, attributeName string) *string {
	for _, attribute := range attributes {
		if *attribute.Name == attributeName {
			return attribute.Value
		}
	}
	return nil
}

func (c *ClusterDetails) getContainerInstances() error {
	c.ContainerInstances = make([]*ContainerInstance, 0)
	reqContainerInstances := ecs.ListContainerInstancesInput{Cluster: c.ClusterArn}
	resContainerInstances, err := ecsService.ListContainerInstances(&reqContainerInstances)

	if err != nil {
		logrus.Error(err)
		return errors.Wrap(err, 1)
	}

	if len(resContainerInstances.ContainerInstanceArns) > 0 {
		reqDescribeContainerInstances := ecs.DescribeContainerInstancesInput{Cluster: c.ClusterArn, ContainerInstances: resContainerInstances.ContainerInstanceArns}
		resDescribeContainerInstances, err := ecsService.DescribeContainerInstances(&reqDescribeContainerInstances)

		if err != nil {
			logrus.Error(err)
			return errors.Wrap(err, 1)
		}

		for _, containerInstance := range resDescribeContainerInstances.ContainerInstances {
			var container ContainerInstance
			container.ContainerInstanceArn = containerInstance.ContainerInstanceArn
			container.EC2InstanceId = containerInstance.Ec2InstanceId
			container.RegisteredDate = containerInstance.RegisteredAt
			container.Status = containerInstance.Status
			container.AgentConnected = containerInstance.AgentConnected
			container.TotalCPU = getResourceValue(containerInstance.RegisteredResources, "CPU")
			container.TotalMemory = getResourceValue(containerInstance.RegisteredResources, "MEMORY")
			container.RemainingCPU = getResourceValue(containerInstance.RemainingResources, "CPU")
			container.RemainingMemory = getResourceValue(containerInstance.RemainingResources, "MEMORY")
			container.RunningTasksCount = containerInstance.RunningTasksCount
			container.PendingTasksCount = containerInstance.PendingTasksCount
			container.AvailabilityZone = getAttributeValue(containerInstance.Attributes, "ecs.availability-zone")
			c.ContainerInstances = append(c.ContainerInstances, &container)
		}
	}

	return nil
}

func (c *ClusterDetails) getTasks() error {
	c.Tasks = make([]*Task, 0)
	req := ecs.ListTasksInput{Cluster: c.ClusterArn}
	res, err := ecsService.ListTasks(&req)

	if err != nil {
		logrus.Error(err)
		return errors.Wrap(err, 1)
	}

	if len(res.TaskArns) > 0 {
		reqTaskdetails := ecs.DescribeTasksInput{Cluster: c.ClusterArn, Tasks: res.TaskArns}
		resTaskDetails, err := ecsService.DescribeTasks(&reqTaskdetails)

		if err != nil {
			logrus.Error(err)
			return errors.Wrap(err, 1)
		}
		for _, task := range resTaskDetails.Tasks {
			var clusterTask Task
			clusterTask.ContainerInstanceArn = task.ContainerInstanceArn
			clusterTask.TaskArn = task.TaskArn
			clusterTask.Status = task.LastStatus
			clusterTask.DesiredStatus = task.DesiredStatus

			parseCPU, err := strconv.Atoi(*task.Cpu)
			if err != nil {
				logrus.Error(err)
				return errors.Wrap(err, 1)
			}
			clusterTask.CPU = &parseCPU

			parseMemory, err := strconv.Atoi(*task.Memory)
			if err != nil {
				logrus.Error(err)
				return errors.Wrap(err, 1)
			}
			clusterTask.CPU = &parseMemory

			c.Tasks = append(c.Tasks, &clusterTask)
		}
	}
	return nil
}

func (c *ClusterDetails) getServices() error {
	c.Services = make([]*Service, 0)
	req := ecs.ListServicesInput{Cluster: c.ClusterArn}
	res, err := ecsService.ListServices(&req)

	if err != nil {
		logrus.Error(err)
		return errors.Wrap(err, 1)
	}

	if len(res.ServiceArns) > 0 {
		reqServiceDetails := ecs.DescribeServicesInput{Cluster: c.ClusterArn, Services: res.ServiceArns}
		resServiceDetails, err := ecsService.DescribeServices(&reqServiceDetails)

		if err != nil {
			logrus.Error(err)
			return errors.Wrap(err, 1)
		}
		for _, service := range resServiceDetails.Services {
			var clusterService Service
			clusterService.ServiceArn = service.ServiceArn
			clusterService.CurrentTaskCount = service.RunningCount
			clusterService.DesiredTaskCount = service.DesiredCount
			clusterService.PendingTaskCount = service.PendingCount
			clusterService.Events = make([]*ServiceEvent, 0)
			for _, event := range service.Events {
				var serviceEvent ServiceEvent
				serviceEvent.Id = event.Id
				serviceEvent.Message = event.Message
				serviceEvent.CreatedAt = event.CreatedAt
				clusterService.Events = append(clusterService.Events, &serviceEvent)
			}
			c.Services = append(c.Services, &clusterService)
		}
	}
	return nil
}

func (c *ClusterDetails) getAutoScalingGroups() error {

	if len(c.ContainerInstances) == 0 {
		return nil
	}

	instanceIds := make([]*string, 0)
	for _, containerInstance := range c.ContainerInstances {
		instanceIds = append(instanceIds, containerInstance.EC2InstanceId)
	}

	//describe the cluster instances in the cluster to try and find the autoscaling group they belong to
	res, err := autoscalingService.DescribeAutoScalingInstances(&autoscaling.DescribeAutoScalingInstancesInput{InstanceIds: instanceIds})

	if err != nil {
		logrus.Error(err)
		return errors.Wrap(err, 1)
	}

	if len(res.AutoScalingInstances) == 0 {
		logrus.Error("Could not find AutoScaling group")
	} else {
		resDescribeAutoScalingGroups, err := autoscalingService.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{AutoScalingGroupNames: []*string{res.AutoScalingInstances[0].AutoScalingGroupName}})

		if err != nil {
			logrus.Error(err)
			return errors.Wrap(err, 1)
		}
		if len(resDescribeAutoScalingGroups.AutoScalingGroups) == 1 {
			autoScalingGroup := resDescribeAutoScalingGroups.AutoScalingGroups[0]
			c.AutoScalingGroup = &AutoScalingGroupDetails{
				Name:                 autoScalingGroup.AutoScalingGroupName,
				AutoScalingGroupArn:  autoScalingGroup.AutoScalingGroupARN,
				DesiredInstanceCount: autoScalingGroup.DesiredCapacity,
				MaxInstanceCount:     autoScalingGroup.MaxSize,
				MinInstanceCount:     autoScalingGroup.MinSize,
			}
		} else {
			logrus.Error("Could not find autoscaling group")
		}
	}

	return nil
}

func (c *ClusterDetails) GetContainerInstance(containerInstanceArn *string) *ContainerInstance {
	for _, instance := range c.ContainerInstances {
		if *instance.ContainerInstanceArn == *containerInstanceArn {
			return instance
		}
	}
	return nil
}


func (c *ClusterDetails) IncreaseClusterCapacity() error {
	newDesiredCapacity := *c.AutoScalingGroup.DesiredInstanceCount + 1

	if newDesiredCapacity > *c.AutoScalingGroup.MaxInstanceCount {
		logrus.Error("Maximum Instance Capacity exceeded")
		return nil
	}

	req := &autoscaling.UpdateAutoScalingGroupInput{DesiredCapacity: &newDesiredCapacity, AutoScalingGroupName: c.AutoScalingGroup.Name}
	logrus.WithFields(logrus.Fields{
		"AutoScalingGroupName": *req.AutoScalingGroupName,
		"DesiredCapacity":      *req.DesiredCapacity,
	}).Info("Increasing Cluster Capacity")

	_, err := autoscalingService.UpdateAutoScalingGroup(req)

	if err != nil {
		logrus.Error(err)
		return errors.Wrap(err, 1)
	}

	return nil
}


func (c *ClusterDetails) StandByClusterInstance(containerInstanceArn *string) (*string, error) {

	var containerInstance = c.GetContainerInstance(containerInstanceArn)

	logrus.WithFields(logrus.Fields{
		"ClusterArn":           *c.ClusterArn,
		"ContainerInstanceARN": *containerInstanceArn,
	}).Info("Placing Instance in Standby")

	var shouldDecrement = false
	_, err := autoscalingService.EnterStandby(&autoscaling.EnterStandbyInput{AutoScalingGroupName: c.AutoScalingGroup.Name, InstanceIds: []*string{containerInstance.EC2InstanceId}, ShouldDecrementDesiredCapacity: &shouldDecrement})

	if err != nil {
		logrus.Error(err)
		return nil, errors.Wrap(err, 1)
	}

	return containerInstanceArn, nil
}

func (c *ClusterDetails) DrainClusterInstance(containerInstanceArn *string) (*string, error) {
	var drainCandidateContainerInstanceArn *string
	instanceStillActive := false
	var instance *ContainerInstance
	for _, instanceMember := range c.ContainerInstances {
		if instance == nil {
			instance = instanceMember
		} else if *instanceMember.RunningTasksCount < *instance.RunningTasksCount {
			instance = instanceMember
		}
		if containerInstanceArn != nil && *instanceMember.ContainerInstanceArn == *containerInstanceArn {
			instanceStillActive = true
		}
	}
	drainCandidateContainerInstanceArn = instance.ContainerInstanceArn

	if !instanceStillActive {
		containerInstanceArn = drainCandidateContainerInstanceArn
	}

	logrus.WithFields(logrus.Fields{
		"ClusterArn":           *c.ClusterArn,
		"ContainerInstanceARN": *containerInstanceArn,
	}).Info("Draining Cluster Instance")

	instanceState := "DRAINING"
	_, err := ecsService.UpdateContainerInstancesState(&ecs.UpdateContainerInstancesStateInput{ContainerInstances: []*string{containerInstanceArn}, Status: &instanceState, Cluster: c.ClusterArn})

	if err != nil {
		logrus.Error(err)
		return nil, errors.Wrap(err, 1)
	}

	return containerInstanceArn, nil
}

func (c *ClusterDetails) RemoveClusterInstance(containerInstanceArn *string) error {
	instance := c.GetContainerInstance(containerInstanceArn)
	logrus.WithFields(logrus.Fields{
		"ClusterArn": *c.ClusterArn,
		"InstanceId": *instance.EC2InstanceId,
	}).Info("Removing Cluster Instance")

	trueAddress := true
	_, err := autoscalingService.DetachInstances(&autoscaling.DetachInstancesInput{AutoScalingGroupName: c.AutoScalingGroup.Name, InstanceIds: []*string{instance.EC2InstanceId}, ShouldDecrementDesiredCapacity: &trueAddress})

	if err != nil {
		logrus.Error(err)
		return errors.Wrap(err, 1)
	}

	_, terminateErr := ec2Service.TerminateInstances(&ec2.TerminateInstancesInput{InstanceIds: []*string{instance.EC2InstanceId}})
	if terminateErr != nil {
		logrus.Error(terminateErr)
		return errors.Wrap(terminateErr, 1)
	}

	return nil
}

//GetClusters returns the clusters in the current account
func GetClusters() ([]*ClusterDetails, error) {
	var clusters []*ClusterDetails

	res, err := ecsService.ListClusters(nil)
	if err != nil {
		return nil, errors.Wrap(err, 1)
	}

	reqDescribeClusters := ecs.DescribeClustersInput{Clusters: res.ClusterArns}
	resCluster, err := ecsService.DescribeClusters(&reqDescribeClusters)

	for _, clusterRes := range resCluster.Clusters {
		var cluster ClusterDetails
		cluster.ClusterArn = clusterRes.ClusterArn
		cluster.TotalPendingTasks = clusterRes.PendingTasksCount
		cluster.TotalRunningTasks = clusterRes.RunningTasksCount
		err := cluster.getContainerInstances()
		if err != nil {
			return nil, errors.Wrap(err, 1)
		}
		for _, containerInstance := range cluster.ContainerInstances {
			cluster.TotalCPU = *containerInstance.TotalCPU + cluster.TotalCPU
			cluster.TotalMemory = *containerInstance.TotalMemory + cluster.TotalMemory
			cluster.TotalRemainingCPU = *containerInstance.RemainingCPU + cluster.TotalRemainingCPU
			cluster.TotalRemainingMemory = *containerInstance.RemainingMemory + cluster.TotalRemainingMemory
		}

		err = cluster.getServices()
		if err != nil {
			return nil, errors.Wrap(err, 1)
		}

		err = cluster.getTasks()
		if err != nil {
			return nil, errors.Wrap(err, 1)
		}

		err = cluster.getAutoScalingGroups()
		if err != nil {
			return nil, errors.Wrap(err, 1)
		}

		clusters = append(clusters, &cluster)
	}

	return clusters, nil
}
