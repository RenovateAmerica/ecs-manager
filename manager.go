package main

import (
	"regexp"
	"git.renovateamerica.com/rsi/ra.services.ecsmanager/alert"
	"git.renovateamerica.com/rsi/ra.services.ecsmanager/ecs"
	"git.renovateamerica.com/rsi/ra.services.ecsmanager/config"
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
			logrus.Info("Creating Alert: ", alert)
			alerts = append(alerts, alert)
		}
	} else if percentUtilization < *config.GetConfigValueAsFloat64("ResourceRemoveThresholdPercent") {
		if clusterResourcesSupportDownScale(cluster) {
			alert := alert.NewAlert(alert.ScaleDown, alert.Resources, *cluster.ClusterArn , "")
			logrus.Info("Creating Alert: ", alert)
			alerts = append(alerts, alert)
		}
	}

	//calculate the aggregate percentage of memory utilization
	percentUtilization = round(1-(float64(cluster.TotalRemainingMemory)/float64(cluster.TotalMemory)), .01)
	if percentUtilization > *config.GetConfigValueAsFloat64("ResourceAddThresholdPercent") {
		if clusterResourcesSupportUpScale(cluster) {
			alert := alert.NewAlert(alert.ScaleUp, alert.Resources, *cluster.ClusterArn , "")
			logrus.Info("Creating Alert: ", alert)
			alerts = append(alerts, alert)
		}
	} else if percentUtilization < *config.GetConfigValueAsFloat64("ResourceRemoveThresholdPercent") {
		if clusterResourcesSupportDownScale(cluster) {
			alert := alert.NewAlert(alert.ScaleDown, alert.Resources, *cluster.ClusterArn , "")
			logrus.Info("Creating Alert: ", alert)
			alerts = append(alerts, alert)
		}
	}

	return alerts
}

func checkAllInstancesState(cluster *ecs.ClusterDetails) []*alert.Alert {
	alerts := make([]*alert.Alert, 0)

	for _, clusterInstance := range cluster.ContainerInstances {
		expiredDate := clusterInstance.RegisteredDate.AddDate(0, 0, 7)
		if *clusterInstance.AgentConnected == false {
			alert := alert.NewAlert(alert.Retire, alert.Instance, *cluster.ClusterArn , *clusterInstance.ContainerInstanceArn)
			logrus.Info("Creating Alert: ", alert)
			alerts = append(alerts, alert)
		} else if expiredDate.Before(time.Now()) {
			alert := alert.NewAlert(alert.Retire, alert.Instance, *cluster.ClusterArn , *clusterInstance.ContainerInstanceArn)
			logrus.Info("Creating Alert: ", alert)
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
					logrus.Info("Creating Alert: ", alert)
					alerts = append(alerts, alert)
				}
			}
		}
	}
	return alerts
}

func (ecsCluster *ECSCluster) reconcileAlerts() {

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
		//check to see if we are ready to go inProgress
		if scaleUpAlerts[0].Status == alert.Pending && scaleUpAlerts[0].EventCount > *config.GetConfigValueAsInt64("AlertIntervalCount") {
			ecsCluster.ClusterDetails.IncreaseClusterCapacity()
			scaleUpAlerts[0].Status = alert.InProgress
			scaleUpAlerts[0].EventCount = 0
		} else if scaleUpAlerts[0].Status == alert.InProgress {
			if int64(len(ecsCluster.ClusterDetails.ContainerInstances)) == *ecsCluster.ClusterDetails.AutoScalingGroup.DesiredInstanceCount {
				scaleUpAlerts[0].Status = alert.Completed
				scaleUpAlerts[0].EventCount = 0
			} else {
				logrus.Info("Still adding instances")
			}
		} else if scaleUpAlerts[0].Status == alert.Completed && scaleUpAlerts[0].EventCount > *config.GetConfigValueAsInt64("AlertCooldownIntervalCount") {
			scaleUpAlerts = alert.DeleteAlertFromArray(scaleUpAlerts, 0)
		}
	} else if len(scaleDownAlerts) > 0 {
		if scaleDownAlerts[0].Status == alert.Pending && scaleDownAlerts[0].EventCount > *config.GetConfigValueAsInt64("AlertIntervalCount") {
			var instanceArn *string
			if len(retireAlerts) > 0 {
				instanceArn	= &retireAlerts[0].TargetInstanceArn
			}
			res, _ := ecsCluster.ClusterDetails.DrainClusterInstance(instanceArn)
			scaleDownAlerts[0].TargetInstanceArn = *res
			scaleDownAlerts[0].Status = alert.InProgress
			scaleDownAlerts[0].EventCount = 0

		} else if scaleDownAlerts[0].Status == alert.InProgress {
			if *ecsCluster.ClusterDetails.GetTaskCount(&scaleDownAlerts[0].TargetInstanceArn) == 0 {
				ecsCluster.ClusterDetails.RemoveClusterInstance(&scaleDownAlerts[0].TargetInstanceArn)
				scaleDownAlerts[0].Status = alert.Completed
				scaleDownAlerts[0].EventCount = 0
			} else {
				logrus.Info("Still draining instances")
			}
		} else if scaleDownAlerts[0].Status == alert.Completed && scaleDownAlerts[0].EventCount > *config.GetConfigValueAsInt64("AlertCooldownIntervalCount")  {
			scaleDownAlerts = alert.DeleteAlertFromArray(scaleDownAlerts, 0)
		}
	} else if len(retireAlerts) > 0 {
		if retireAlerts[0].Status == alert.Pending && retireAlerts[0].EventCount > *config.GetConfigValueAsInt64("AlertIntervalCount") {
			ecsCluster.ClusterDetails.IncreaseClusterCapacity()
			retireAlerts[0].Status = alert.InProgress
			retireAlerts[0].EventCount = 0
		} else if retireAlerts[0].Status == alert.InProgress {
			if int64(len(ecsCluster.ClusterDetails.ContainerInstances)) == *ecsCluster.ClusterDetails.AutoScalingGroup.DesiredInstanceCount {
				retireAlerts[0].Status = alert.Completed
				retireAlerts[0].EventCount = 0
			} else {
				logrus.Info("Still adding instances")
			}
		} else if retireAlerts[0].Status == alert.Completed && retireAlerts[0].EventCount > *config.GetConfigValueAsInt64("AlertCooldownIntervalCount")  {
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
