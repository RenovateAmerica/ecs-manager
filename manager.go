package main

import (
	"regexp"
	"github.com/sd-charris/ecs-manager/alert"
	"github.com/sd-charris/ecs-manager/config"
	"github.com/sd-charris/ecs-manager/ecs"
	"github.com/sirupsen/logrus"
	"sort"
	"time"
)

type ECSCluster struct {
	ClusterDetails *ecs.ClusterDetails
	Alerts         []*alert.Alert
}

func round(x, unit float64) float64 {
	return float64(int64(x/unit+0.5)) * unit
}

func clusterResourcesSupportUpScale(cluster *ecs.ClusterDetails) bool {
	boxSize := cluster.ContainerInstances[0].TotalCPU
	newTotal := cluster.TotalCPU + *boxSize
	percentUtilization := round(1-(float64(cluster.TotalRemainingCPU + *boxSize)/float64(newTotal)), .01)
	if percentUtilization > *config.GetConfigValueAsFloat64("ResourceRemoveThresholdPercent") {
		return true
	}

	boxSize = cluster.ContainerInstances[0].TotalMemory
	newTotal = cluster.TotalMemory + *boxSize
	percentUtilization = round(1-(float64(cluster.TotalRemainingMemory + *boxSize)/float64(newTotal)), .01)
	if percentUtilization > *config.GetConfigValueAsFloat64("ResourceRemoveThresholdPercent") {
		return true
	}
	return false
}

func clusterResourcesSupportDownScale(cluster *ecs.ClusterDetails) bool {
	boxSize := cluster.ContainerInstances[0].TotalCPU
	newTotal := cluster.TotalCPU - *boxSize
	percentUtilization := round(1-(float64(cluster.TotalRemainingCPU - *boxSize)/float64(newTotal)), .01)
	if percentUtilization > *config.GetConfigValueAsFloat64("ResourceAddThresholdPercent") {
		return false
	}

	boxSize = cluster.ContainerInstances[0].TotalMemory
	newTotal = cluster.TotalMemory - *boxSize
	percentUtilization = round(1-(float64(cluster.TotalRemainingMemory - *boxSize)/float64(newTotal)), .01)
	if percentUtilization > *config.GetConfigValueAsFloat64("ResourceAddThresholdPercent") {
		return false
	}
	return true
}

func checkClusterResources(cluster *ecs.ClusterDetails) []*alert.Alert {
	alerts := make([]*alert.Alert, 0)

	//calculate the aggregate percentage of cpu utilization
	percentUtilization := round(1-(float64(cluster.TotalRemainingCPU)/float64(cluster.TotalCPU)), .01)
	if percentUtilization > *config.GetConfigValueAsFloat64("ResourceAddThresholdPercent") {
		if clusterResourcesSupportUpScale(cluster) {
			alert := alert.NewAlert(alert.ScaleUp, alert.Resources, *cluster.ClusterArn , "")
			logrus.WithFields(logrus.Fields{
				"Alert":    alert,
			}).Info("Creating Alert")
			alerts = append(alerts, alert)
		}
	} else if percentUtilization < *config.GetConfigValueAsFloat64("ResourceRemoveThresholdPercent") {
		if clusterResourcesSupportDownScale(cluster) {
			alert := alert.NewAlert(alert.ScaleDown, alert.Resources, *cluster.ClusterArn , "")
			logrus.WithFields(logrus.Fields{
				"Alert":    alert,
			}).Info("Creating Alert")
			alerts = append(alerts, alert)
		}
	}

	//calculate the aggregate percentage of memory utilization
	percentUtilization = round(1-(float64(cluster.TotalRemainingMemory)/float64(cluster.TotalMemory)), .01)
	if percentUtilization > *config.GetConfigValueAsFloat64("ResourceAddThresholdPercent") {
		if clusterResourcesSupportUpScale(cluster) {
			alert := alert.NewAlert(alert.ScaleUp, alert.Resources, *cluster.ClusterArn , "")
			logrus.WithFields(logrus.Fields{
				"Alert":    alert,
			}).Info("Creating Alert")
			alerts = append(alerts, alert)
		}
	} else if percentUtilization < *config.GetConfigValueAsFloat64("ResourceRemoveThresholdPercent") {
		if clusterResourcesSupportDownScale(cluster) {
			alert := alert.NewAlert(alert.ScaleDown, alert.Resources, *cluster.ClusterArn , "")
			logrus.WithFields(logrus.Fields{
				"Alert":    alert,
			}).Info("Creating Alert")
			alerts = append(alerts, alert)
		}
	}

	return alerts
}

func checkAllInstancesState(cluster *ecs.ClusterDetails) []*alert.Alert {
	alerts := make([]*alert.Alert, 0)
	var instanceAge = int(*config.GetConfigValueAsInt64("InstanceMaxAgeDays"))

	for _, clusterInstance := range cluster.ContainerInstances {
		expiredDate := clusterInstance.RegisteredDate.AddDate(0, 0, instanceAge)
		if *clusterInstance.AgentConnected == false {
			alert := alert.NewAlert(alert.Retire, alert.Instance, *cluster.ClusterArn , *clusterInstance.ContainerInstanceArn)
			logrus.WithFields(logrus.Fields{
				"Alert":    alert,
			}).Info("Creating Alert")
			alerts = append(alerts, alert)
		} else if expiredDate.Before(time.Now()) {
			alert := alert.NewAlert(alert.Retire, alert.Instance, *cluster.ClusterArn , *clusterInstance.ContainerInstanceArn)
			logrus.WithFields(logrus.Fields{
				"Alert":    alert,
			}).Info("Creating Alert")
			alerts = append(alerts, alert)
		} else if *clusterInstance.Status == "DRAINING" {
			alert := alert.NewAlert(alert.Retire, alert.Instance, *cluster.ClusterArn , *clusterInstance.ContainerInstanceArn)
			logrus.WithFields(logrus.Fields{
				"Alert":    alert,
			}).Info("Creating Alert")
			alerts = append(alerts, alert)
		}
	}
	return alerts
}

func checkServicesDesiredCount(cluster *ecs.ClusterDetails) []*alert.Alert {
	alerts := make([]*alert.Alert, 0)
	r, _ := regexp.Compile(".*(insufficient).*(available).*") //need to find a better way to identify if there is a provisioning limit issue

	for _, service := range cluster.Services {
		if *service.DesiredTaskCount > (*service.CurrentTaskCount + *service.PendingTaskCount) {
			if len(service.Events) > 0 {
				lastMessage := *service.Events[0].Message
				if r.MatchString(lastMessage) {
					alert := alert.NewAlert(alert.ScaleUp, alert.Service, *cluster.ClusterArn , "")
					logrus.WithFields(logrus.Fields{
						"Alert":    alert,
					}).Info("Creating Alert")
					alerts = append(alerts, alert)
				}
			}
		}
	}
	return alerts
}

func (ecsCluster *ECSCluster) reconcileAlerts() {

	alertIntervalCount := config.GetConfigValueAsInt64("AlertIntervalCount")
	alertCoolDownIntervalCount := config.GetConfigValueAsInt64("AlertCooldownIntervalCount")
	scaleUpAlerts := make([]*alert.Alert, 0)
	scaleDownAlerts := make([]*alert.Alert, 0)
	retireAlerts := make([]*alert.Alert, 0)

	//order by date
	sort.Slice(ecsCluster.Alerts, func(i, j int) bool {
		return ecsCluster.Alerts[i].EventCount > ecsCluster.Alerts[j].EventCount
	})

	//group up alerts by their type, status, and trigger
	for _, alertItem := range ecsCluster.Alerts {
		if alertItem.Type == alert.ScaleUp  {
			scaleUpAlerts = append(scaleUpAlerts, alertItem)
		}

		if alertItem.Type == alert.ScaleDown {
			scaleDownAlerts = append(scaleDownAlerts, alertItem)
		}

		if alertItem.Type == alert.Retire {
			retireAlerts = append(retireAlerts, alertItem)
		}
	}

	// if there a scale up event
	if len(scaleUpAlerts) > 0 {
		currentScaleUpAlert := scaleUpAlerts[0]
		if currentScaleUpAlert.Status == alert.Pending && currentScaleUpAlert.EventCount > *alertIntervalCount {
			ecsCluster.ClusterDetails.IncreaseClusterCapacity()
			currentScaleUpAlert.Status = alert.InProgress
			currentScaleUpAlert.EventCount = 0
		} else if currentScaleUpAlert.Status == alert.InProgress {
			if int64(len(ecsCluster.ClusterDetails.ContainerInstances)) == *ecsCluster.ClusterDetails.AutoScalingGroup.DesiredInstanceCount {
				currentScaleUpAlert.Status = alert.Completed
				currentScaleUpAlert.EventCount = 0
			} else {
				logrus.Info("Still adding instances")
			}
		} else if currentScaleUpAlert.Status == alert.Completed && currentScaleUpAlert.EventCount > *alertCoolDownIntervalCount {
			scaleUpAlerts = alert.DeleteAlertFromArray(scaleUpAlerts, 0)
		}
	} else if len(scaleDownAlerts) > 0 {
		currentScaleDownAlerts := scaleDownAlerts[0]
		if currentScaleDownAlerts.Status == alert.Pending && currentScaleDownAlerts.EventCount > *alertIntervalCount {
			var containerInstanceArn *string
			if len(retireAlerts) > 0 {
				currentRetireAlert := retireAlerts[0]
				containerInstanceArn = &currentRetireAlert.ContainerInstanceArn
			}
			res, _ := ecsCluster.ClusterDetails.DrainClusterInstance(containerInstanceArn)
			currentScaleDownAlerts.ContainerInstanceArn = *res
			currentScaleDownAlerts.Status = alert.InProgress
			currentScaleDownAlerts.EventCount = 0

		} else if currentScaleDownAlerts.Status == alert.InProgress {
			containerInstance := ecsCluster.ClusterDetails.GetContainerInstance(&currentScaleDownAlerts.ContainerInstanceArn)
			if containerInstance != nil && *containerInstance.RunningTasksCount == 0 {
				ecsCluster.ClusterDetails.RemoveClusterInstance(containerInstance.ContainerInstanceArn)
				currentScaleDownAlerts.Status = alert.Completed
				currentScaleDownAlerts.EventCount = 0
			} else {
				logrus.Info("Still draining instances")
			}
		} else if currentScaleDownAlerts.Status == alert.Completed && currentScaleDownAlerts.EventCount > *alertCoolDownIntervalCount  {
			scaleDownAlerts = alert.DeleteAlertFromArray(scaleDownAlerts, 0)
		}
	} else if len(retireAlerts) > 0 {
		currentRetireAlert := retireAlerts[0]
		if currentRetireAlert.Status == alert.Pending && currentRetireAlert.EventCount > *alertIntervalCount {
			ecsCluster.ClusterDetails.StandByClusterInstance(&currentRetireAlert.ContainerInstanceArn)
			ecsCluster.ClusterDetails.IncreaseClusterCapacity()
			currentRetireAlert.Status = alert.InProgress
			currentRetireAlert.EventCount = 0
		} else if currentRetireAlert.Status == alert.InProgress {
			if int64(len(ecsCluster.ClusterDetails.ContainerInstances)) >= *ecsCluster.ClusterDetails.AutoScalingGroup.DesiredInstanceCount {
				currentRetireAlert.Status = alert.Completed
				currentRetireAlert.EventCount = 0
			} else {
				logrus.Info("Still adding instances")
			}
		} else if currentRetireAlert.Status == alert.Completed && currentRetireAlert.EventCount > *alertCoolDownIntervalCount {
			retireAlerts = alert.DeleteAlertFromArray(retireAlerts, 0)
		}
	}

	response := make([]*alert.Alert, 0)
	if len(scaleUpAlerts) > 0 {
		response = append(response, scaleUpAlerts...)
	}
	if len(scaleDownAlerts) > 0 {
		response = append(response, scaleDownAlerts...)
	}
	if len(retireAlerts) > 0 {
		response = append(response, retireAlerts...)
	}
	ecsCluster.Alerts = response
}
