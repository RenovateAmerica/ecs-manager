package alert

import (
	"sort"
	"time"
	"fmt"
)

type Type int

const (
	ScaleUp   Type = iota
	ScaleDown
	Retire
)

type Status int

const (
	Created    Status = iota
	Pending
	InProgress
	Completed
)

type Trigger int

const (
	Resources	Trigger = iota
	Schedule
	Service
	Instance
)

type Alert struct {
	Type              Type
	Status            Status
	Trigger           Trigger
	EventCount        int64
	ClusterArn        string
	ContainerInstanceArn string
	AlertDate         time.Time
	LastActionDate    time.Time
}

func (a Alert) String() string{
	alertType := "?"
	switch  a.Type{
	case ScaleUp:
		alertType = "ScaleUp"
	case ScaleDown:
		alertType = "ScaleDown"
	case Retire:
		alertType = "Retire"
	}

	alertStatus := "?"
	switch a.Status{
	case Created:
		alertStatus = "Created"
	case Pending:
		alertStatus = "Pending"
	case InProgress:
		alertStatus = "InProgress"
	case Completed:
		alertStatus = "Completed"
	}

	alertTrigger := "?"
	switch a.Trigger{
	case Resources:
		alertTrigger = "Resources"
	case Schedule:
		alertTrigger = "Schedule"
	case Service:
		alertTrigger = "Service"
	case Instance:
		alertTrigger = "Instance"
	}

	return fmt.Sprintf("Cluster: %s Count: %d AlertType: %s AlertTrigger: %s AlertStatus: %s Instance: %s", a.ClusterArn, a.EventCount, alertType, alertTrigger, alertStatus, a.ContainerInstanceArn)
}


func NewAlert(alertType Type, alertTrigger Trigger, clusterArn string, containerInstanceArn string) *Alert {
	return &Alert{
		Type:              alertType,
		Status:            Created,
		Trigger:           alertTrigger,
		EventCount:        1,
		ClusterArn:        clusterArn,
		ContainerInstanceArn: containerInstanceArn,
		AlertDate:         time.Now(),
		LastActionDate:    time.Now(),
	}
}

func DeleteAlertFromArray(alerts []*Alert, i int) []*Alert {
	copy(alerts[i:], alerts[i+1:])
	alerts[len(alerts)-1] = nil // or the zero value of T
	alerts = alerts[:len(alerts)-1]
	return alerts
}

func AlertsContainInstanceArn(alerts []*Alert, instanceArn string) bool {
	for _, alert := range alerts {
		if alert.ContainerInstanceArn == instanceArn {
			return true
		}
	}
	return false
}

func ConsolidateAlerts(alerts []*Alert) []*Alert {
	response := make([]*Alert, 0)
	if len(alerts) == 0 {
		return response
	}

	newScaleUpAlerts := make([]*Alert, 0)
	newScaleDownAlerts := make([]*Alert, 0)
	newRetireAlerts := make([]*Alert, 0)
	reOccurringAlerts := make([]*Alert, 0)

	scaleUpPending := false
	scaleDownPending := false
	retirePending := false

	//order by date
	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].AlertDate.Before(alerts[j].AlertDate)
	})

	//group up alerts by their type, status, and trigger
	for _, alertItem := range alerts {

		if alertItem.Type == ScaleUp && alertItem.Status == Created {
			newScaleUpAlerts = append(newScaleUpAlerts, alertItem)
		}

		if alertItem.Type == ScaleDown && alertItem.Status == Created {
			newScaleDownAlerts = append(newScaleDownAlerts, alertItem)
		}

		if alertItem.Type == Retire && alertItem.Status == Created {
			newRetireAlerts = append(newRetireAlerts, alertItem)
		}

		if alertItem.Status != Created {
			reOccurringAlerts = append(reOccurringAlerts, alertItem)
			if alertItem.Type == ScaleUp {
				scaleUpPending = true
			}
			if alertItem.Type == ScaleDown {
				scaleDownPending = true
			}
			if alertItem.Type == Retire {
				retirePending = true
			}
		}
	}

	//check if there are any re-occurring events and if they need to be marked as incremented or removed
	i := 0
	if len(reOccurringAlerts) > 0 {
		for _, alert := range reOccurringAlerts {
			alert.EventCount += 1
			if alert.Type == ScaleUp && alert.Status == Pending {
				if len(newScaleUpAlerts) == 0 {
					reOccurringAlerts = DeleteAlertFromArray(reOccurringAlerts, i)
					i-=1
				}
			} else if alert.Type == ScaleDown && alert.Status == Pending {
				if len(newScaleDownAlerts) == 0 {
					reOccurringAlerts = DeleteAlertFromArray(reOccurringAlerts, i)
					i-=1
				}
			} else if alert.Type == Retire && alert.Status == Pending {
				if !AlertsContainInstanceArn(newRetireAlerts, alert.ContainerInstanceArn) {
					reOccurringAlerts = DeleteAlertFromArray(reOccurringAlerts, i)
					i-=1
				}
			}
			i += 1
		}
		if len(reOccurringAlerts) > 0 {
			response = append(response, reOccurringAlerts...)
		}
	}

	if len(newScaleUpAlerts) > 0 && !scaleUpPending {
		scaleUpPending = true
		newScaleUpAlerts[0].Status = Pending
		response = append(response, newScaleUpAlerts[0])
	}

	if len(newScaleDownAlerts) > 0 && !scaleDownPending{
		scaleDownPending = true
		newScaleDownAlerts[0].Status = Pending
		response = append(response, newScaleDownAlerts[0])
	}

	if len(newRetireAlerts) > 0 && !retirePending{
		retirePending = true
		newRetireAlerts[0].Status = Pending
		response = append(response, newRetireAlerts[0])
	}

	return response
}
