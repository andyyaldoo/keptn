package controller

import (
	"fmt"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	keptn "github.com/keptn/go-utils/pkg/lib"
	keptnv2 "github.com/keptn/go-utils/pkg/lib/v0_2_0"
	"github.com/keptn/keptn/helm-service/pkg/configuration_changer"
	"strconv"
)

// ActionTriggeredHandler handles sh.keptn.events.action.triggered events for scaling
type ActionTriggeredHandler struct {
	Handler
}

// ActionScaling is the identifier for the scaling action
const ActionScaling = "scaling"

// NewActionTriggeredHandler creates a new ActionTriggeredHandler
func NewActionTriggeredHandler(keptnHandler *keptnv2.Keptn, configServiceURL string) ActionTriggeredHandler {
	return ActionTriggeredHandler{
		Handler: NewHandlerBase(keptnHandler, configServiceURL),
	}
}

// HandleEvent takes the sh.keptn.events.action.triggered event and performs the scaling action on the generated chart
// Therefore, this scaling action only works if the service is deployed b/g
func (h ActionTriggeredHandler) HandleEvent(ce cloudevents.Event, closeLogger func(keptnHandler *keptnv2.Keptn)) {

	defer closeLogger(h.Handler.GetKeptnHandler())

	actionTriggeredEvent := keptnv2.ActionTriggeredEventData{}

	err := ce.DataAs(&actionTriggeredEvent)
	if err != nil {
		err = fmt.Errorf("failed to unmarshal data: %v", err)
		h.HandleError(ce.ID(), err, keptnv2.ActionTaskName, h.getFinishedEventDataForError(actionTriggeredEvent.EventData, err))
		return
	}

	if actionTriggeredEvent.Action.Action == ActionScaling {
		// Send action.started event
		if sendErr := h.SendEvent(ce.ID(), keptnv2.GetStartedEventType(keptnv2.ActionTaskName), h.getStartedEventData(actionTriggeredEvent.EventData)); sendErr != nil {
			h.HandleError(ce.ID(), err, keptnv2.ActionTaskName, h.getFinishedEventDataForError(actionTriggeredEvent.EventData, err))
			return
		}

		resp := h.handleScaling(actionTriggeredEvent)
		if resp.Status == keptnv2.StatusErrored {
			h.GetKeptnHandler().Logger.Error(fmt.Sprintf("action %s errored with result %s", actionTriggeredEvent.Action.Action, resp.Message))
		} else {
			h.GetKeptnHandler().Logger.Info(fmt.Sprintf("Sucessfully finished action action %s", actionTriggeredEvent.Action.Action))
		}

		// Send action.finished event
		if err := h.SendEvent(ce.ID(), keptnv2.GetFinishedEventType(keptnv2.ActionTaskName), resp); err != nil {
			h.HandleError(ce.ID(), err, keptnv2.ActionTaskName, h.getFinishedEventDataForError(actionTriggeredEvent.EventData, err))
			return
		}
	} else {
		h.GetKeptnHandler().Logger.Info("Received unhandled action: " + actionTriggeredEvent.Action.Action + ". Exiting")
	}

	return
}

func (h ActionTriggeredHandler) getStartedEventData(inEventData keptnv2.EventData) keptnv2.ActionStartedEventData {
	inEventData.Status = keptnv2.StatusSucceeded
	inEventData.Result = ""
	inEventData.Message = ""
	return keptnv2.ActionStartedEventData{
		EventData: inEventData,
	}
}

func (h ActionTriggeredHandler) getFinishedEventDataForSuccess(inEventData keptnv2.EventData, gitCommit string) keptnv2.ActionFinishedEventData {
	inEventData.Status = keptnv2.StatusSucceeded
	inEventData.Result = keptnv2.ResultPass
	inEventData.Message = "Successfully executed scaling action"
	return keptnv2.ActionFinishedEventData{
		EventData: inEventData,
		Action: keptnv2.ActionData{
			GitCommit: gitCommit,
		},
	}
}

func (h ActionTriggeredHandler) getFinishedEventDataForError(eventData keptnv2.EventData, err error) keptnv2.ActionFinishedEventData {

	eventData.Status = keptnv2.StatusErrored
	eventData.Result = keptnv2.ResultFailed
	eventData.Message = err.Error()
	return keptnv2.ActionFinishedEventData{
		EventData: eventData,
	}
}

func (h ActionTriggeredHandler) getFinishedEventData(eventData keptnv2.EventData, status keptnv2.StatusType,
	result keptnv2.ResultType, msg string) keptnv2.ActionFinishedEventData {

	eventData.Status = status
	eventData.Result = result
	eventData.Message = msg
	return keptnv2.ActionFinishedEventData{
		EventData: eventData,
	}
}

func (h ActionTriggeredHandler) handleScaling(e keptnv2.ActionTriggeredEventData) keptnv2.ActionFinishedEventData {

	value, ok := e.Action.Value.(string)
	if !ok {
		return h.getFinishedEventData(e.EventData, keptnv2.StatusSucceeded,
			keptnv2.ResultFailed, "could not parse action.value to string value")
	}
	replicaIncrement, err := strconv.Atoi(value)
	if err != nil {
		return h.getFinishedEventData(e.EventData, keptnv2.StatusSucceeded,
			keptnv2.ResultFailed, "could not parse action.value to int")
	}

	replicaCountUpdater := configuration_changer.NewReplicaCountUpdater(replicaIncrement)
	// Note: This action applies the scaling on the generated chart and therefore assumes a b/g deployment
	genChart, gitVersion, err := configuration_changer.NewConfigurationChanger(h.GetConfigServiceURL()).UpdateChart(e.EventData,
		true, replicaCountUpdater)
	if err != nil {
		return h.getFinishedEventDataForError(e.EventData, err)
	}

	// Upgrade chart
	if err := h.upgradeChart(genChart, e.EventData, keptn.Duplicate); err != nil {
		return h.getFinishedEventDataForError(e.EventData, err)
	}

	return h.getFinishedEventDataForSuccess(e.EventData, gitVersion)
}
