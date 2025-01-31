// Copyright 2019 Yunion
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tasks

import (
	"context"

	"yunion.io/x/jsonutils"
	"yunion.io/x/log"

	api "yunion.io/x/onecloud/pkg/apis/compute"
	"yunion.io/x/onecloud/pkg/cloudcommon/db"
	"yunion.io/x/onecloud/pkg/cloudcommon/db/taskman"
	"yunion.io/x/onecloud/pkg/compute/models"
	"yunion.io/x/onecloud/pkg/util/logclient"
)

type GuestSyncConfTask struct {
	SGuestBaseTask
}

func init() {
	taskman.RegisterTask(GuestSyncConfTask{})
}

func (self *GuestSyncConfTask) OnInit(ctx context.Context, obj db.IStandaloneModel, data jsonutils.JSONObject) {
	guest := obj.(*models.SGuest)
	db.OpsLog.LogEvent(guest, db.ACT_SYNC_CONF, nil, self.UserCred)
	if host, _ := guest.GetHost(); host == nil {
		self.SetStageFailed(ctx, jsonutils.NewString("No host for sync"))
		return
	} else {
		self.SetStage("on_sync_complete", nil)
		if err := guest.GetDriver().RequestSyncConfigOnHost(ctx, guest, host, self); err != nil {
			self.SetStageFailed(ctx, jsonutils.NewString(err.Error()))
			log.Errorf("SyncConfTask faled %v", err)
		}
	}
}

func (self *GuestSyncConfTask) OnSyncComplete(ctx context.Context, obj db.IStandaloneModel, data jsonutils.JSONObject) {
	guest := obj.(*models.SGuest)
	if fwOnly, _ := self.GetParams().Bool("fw_only"); fwOnly {
		db.OpsLog.LogEvent(guest, db.ACT_SYNC_CONF, nil, self.UserCred)
		if restart, _ := self.Params.Bool("restart_network"); !restart {
			self.SetStageComplete(ctx, nil)
			return
		}
		prevIp, err := self.Params.GetString("prev_ip")
		if err != nil {
			log.Errorf("unable to get prev_ip when restart_network is true when sync guest")
			self.SetStageComplete(ctx, nil)
			return
		}
		if inBlockStream := jsonutils.QueryBoolean(self.Params, "in_block_stream", false); inBlockStream {
			guest.StartRestartNetworkTask(ctx, self.UserCred, "", prevIp, true)
		} else {
			guest.StartRestartNetworkTask(ctx, self.UserCred, "", prevIp, false)
		}
		self.SetStageComplete(ctx, guest.GetShortDesc(ctx))
	} else if data.Contains("task") {
		// XXX this is only applied to KVM, which will call task_complete twice
		self.SetStage("on_disk_sync_complete", nil)
	} else {
		self.OnDiskSyncComplete(ctx, guest, data)
	}
}

func (self *GuestSyncConfTask) OnDiskSyncComplete(ctx context.Context, guest *models.SGuest, data jsonutils.JSONObject) {
	if jsonutils.QueryBoolean(self.Params, "without_sync_status", false) {
		self.OnSyncStatusComplete(ctx, guest, nil)
	} else {
		self.SetStage("on_sync_status_complete", nil)
		guest.StartSyncstatus(ctx, self.GetUserCred(), self.GetTaskId())
	}
}

func (self *GuestSyncConfTask) OnDiskSyncCompleteFailed(ctx context.Context, obj db.IStandaloneModel, data jsonutils.JSONObject) {
	guest := obj.(*models.SGuest)
	db.OpsLog.LogEvent(guest, db.ACT_SYNC_CONF_FAIL, data, self.UserCred)
	logclient.AddActionLogWithStartable(self, guest, logclient.ACT_VM_SYNC_CONF, data, self.UserCred, false)
	if !jsonutils.QueryBoolean(self.Params, "without_sync_status", false) {
		guest.SetStatus(self.GetUserCred(), api.VM_SYNC_FAIL, data.String())
	}
	self.SetStageFailed(ctx, data)
}

func (self *GuestSyncConfTask) OnSyncCompleteFailed(ctx context.Context, obj db.IStandaloneModel, data jsonutils.JSONObject) {
	guest := obj.(*models.SGuest)
	if !jsonutils.QueryBoolean(self.Params, "without_sync_status", false) {
		guest.SetStatus(self.GetUserCred(), api.VM_SYNC_FAIL, data.String())
	}
	logclient.AddActionLogWithStartable(self, guest, logclient.ACT_VM_SYNC_CONF, data, self.UserCred, false)
	db.OpsLog.LogEvent(guest, db.ACT_SYNC_CONF_FAIL, data, self.UserCred)
	self.SetStageFailed(ctx, data)
}

func (self *GuestSyncConfTask) OnSyncStatusComplete(ctx context.Context, guest *models.SGuest, data jsonutils.JSONObject) {
	self.SetStageComplete(ctx, nil)
}

func (self *GuestSyncConfTask) OnSyncStatusCompleteFailed(ctx context.Context, guest *models.SGuest, data jsonutils.JSONObject) {
	self.SetStageFailed(ctx, data)
}
