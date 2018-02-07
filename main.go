package main

import (
	"git.renovateamerica.com/rsi/ra.services.ecsmanager/alert"
	"git.renovateamerica.com/rsi/ra.services.ecsmanager/config"
	"git.renovateamerica.com/rsi/ra.services.ecsmanager/ecs"
	"github.com/sd-charris/logrus-cloudwatchlogs"
	"github.com/go-errors/errors"
	"github.com/sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"time"
	"log"
	"strconv"
)

var ecsClusters map[string]*ECSCluster
var logger *logrus.Logger



func main() {
	defer func(){
		err := recover().(error)
		logrus.Error(err)
	}()
	cfg := aws.NewConfig().WithRegion("us-west-2")

	logStreamName := strconv.Itoa(int(time.Now().Unix()))
	logGroupName := "/aws/ecs/manager"
	hook, err := logrus_cloudwatchlogs.NewHook(logGroupName, logStreamName, cfg)
	if err != nil {
		log.Fatal(err)
	}

	logrus.AddHook(hook)

	ecsClusters = make(map[string]*ECSCluster)
	logrus.Info("Pull Configuration")
	config.LoadConfig("./config.json")

	logrus.Info("Configure AWS ECS")
	ecs.Initialize()

	intervalSeconds := time.Duration(*config.GetConfigValueAsInt64("IntervalSeconds"))

	err = start(intervalSeconds * time.Second)
	if err != nil {
		logrus.Error(err.(*errors.Error).ErrorStack())
		panic(err)
	}
}

func start(delay time.Duration) error{
	for {
		select {
		case <-time.After(delay):
			err := process()
			if err != nil {
				logrus.Error(err)
				return errors.Wrap(err, 1)
			}
		}
	}
}

func process() error{
	logrus.Info("------------------------------------------- Start Check -------------------------------------------")
	clusters, err := ecs.GetClusters()


	if err != nil {
		logrus.Error(err)
		return errors.Wrap(err, 1)
	}

	for _, cluster := range clusters {
		if ecsClusters[*cluster.ClusterArn] == nil {
			ecsClusters[*cluster.ClusterArn] = &ECSCluster{}
		}
		ecsClusters[*cluster.ClusterArn].ClusterDetails = cluster
		logrus.Info("---------------------------- Checking Cluster: ", *cluster.ClusterArn)
		if len(cluster.ContainerInstances) > 0 {
			ecsClusters[*cluster.ClusterArn].Alerts = append(ecsClusters[*cluster.ClusterArn].Alerts, checkClusterResources(cluster)...)
			ecsClusters[*cluster.ClusterArn].Alerts = append(ecsClusters[*cluster.ClusterArn].Alerts, checkServicesDesiredCount(cluster)...)
			ecsClusters[*cluster.ClusterArn].Alerts = append(ecsClusters[*cluster.ClusterArn].Alerts, checkAllInstancesState(cluster)...)
			ecsClusters[*cluster.ClusterArn].Alerts = alert.ConsolidateAlerts(ecsClusters[*cluster.ClusterArn].Alerts)

			logrus.Info("Alert Count: ", len(ecsClusters[*cluster.ClusterArn].Alerts))
			for _, alert := range ecsClusters[*cluster.ClusterArn].Alerts {
				logrus.Info("--------------")
				logrus.Info("Alert AlertDate: ", alert.AlertDate)
				logrus.Info("Alert Status: ", alert.Status)
				logrus.Info("Alert Type: ", alert.Type)
				logrus.Info("Alert EventCount: ", alert.EventCount)
				logrus.Info("Alert InstanceArn: ", alert.TargetInstanceArn)
				logrus.Info("Alert ClusterArn: ", alert.ClusterArn)
				logrus.Info("--------------")
			}
			ecsClusters[*cluster.ClusterArn].reconcileAlerts()
		} else {
			logrus.Info("No Cluster Instances")
		}
	}

	return nil
}
