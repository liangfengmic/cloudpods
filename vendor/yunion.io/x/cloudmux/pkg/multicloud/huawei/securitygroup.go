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

package huawei

/*
https://support.huaweicloud.com/usermanual-vpc/zh-cn_topic_0073379079.html
安全组的限制
默认情况下，一个用户可以创建100个安全组。
默认情况下，一个安全组最多只允许拥有50条安全组规则。
默认情况下，一个弹性云服务器或辅助网卡最多只能被添加到5个安全组中。
在创建私网弹性负载均衡时，需要选择弹性负载均衡所在的安全组。请勿删除默认规则或者确保满足以下规则：
出方向：允许发往同一个安全组的报文可以通过，或者允许对端负载均衡器报文通过。
入方向：允许来自同一个安全组的报文可以通过，或者允许对端负载均衡器报文通过。
*/

import (
	"net"

	"yunion.io/x/jsonutils"
	"yunion.io/x/pkg/errors"
	"yunion.io/x/pkg/util/secrules"
	"yunion.io/x/pkg/utils"

	api "yunion.io/x/cloudmux/pkg/apis/compute"
	"yunion.io/x/cloudmux/pkg/cloudprovider"
	"yunion.io/x/cloudmux/pkg/multicloud"
)

type SecurityGroupRule struct {
	Direction       string `json:"direction"`
	Ethertype       string `json:"ethertype"`
	ID              string `json:"id"`
	Description     string `json:"description"`
	SecurityGroupID string `json:"security_group_id"`
	RemoteGroupID   string `json:"remote_group_id"`
}

type SecurityGroupRuleDetail struct {
	Direction       string `json:"direction"`
	Ethertype       string `json:"ethertype"`
	ID              string `json:"id"`
	Description     string `json:"description"`
	PortRangeMax    int64  `json:"port_range_max"`
	PortRangeMin    int64  `json:"port_range_min"`
	Protocol        string `json:"protocol"`
	RemoteGroupID   string `json:"remote_group_id"`
	RemoteIPPrefix  string `json:"remote_ip_prefix"`
	SecurityGroupID string `json:"security_group_id"`
	TenantID        string `json:"tenant_id"`
}

// https://support.huaweicloud.com/api-vpc/zh-cn_topic_0020090615.html
type SSecurityGroup struct {
	multicloud.SSecurityGroup
	HuaweiTags
	region *SRegion

	ID                  string              `json:"id"`
	Name                string              `json:"name"`
	Description         string              `json:"description"`
	VpcID               string              `json:"vpc_id"`
	EnterpriseProjectID string              `json:"enterprise_project_id "`
	SecurityGroupRules  []SecurityGroupRule `json:"security_group_rules"`
}

// 判断是否兼容云端安全组规则
func compatibleSecurityGroupRule(r SecurityGroupRule) bool {
	// 忽略了源地址是安全组的规则
	if len(r.RemoteGroupID) > 0 {
		return false
	}

	// 忽略IPV6
	if r.Ethertype == "IPv6" {
		return false
	}

	return true
}

func (self *SSecurityGroup) GetId() string {
	return self.ID
}

func (self *SSecurityGroup) GetVpcId() string {
	return api.NORMAL_VPC_ID
}

func (self *SSecurityGroup) GetName() string {
	if len(self.Name) > 0 {
		return self.Name
	}
	return self.ID
}

func (self *SSecurityGroup) GetGlobalId() string {
	return self.ID
}

func (self *SSecurityGroup) GetStatus() string {
	return ""
}

func (self *SSecurityGroup) Refresh() error {
	if new, err := self.region.GetSecurityGroupDetails(self.GetId()); err != nil {
		return err
	} else {
		return jsonutils.Update(self, new)
	}
}

func (self *SSecurityGroup) IsEmulated() bool {
	return false
}

func (self *SSecurityGroup) GetDescription() string {
	if self.Description == self.VpcID {
		return ""
	}
	return self.Description
}

// todo: 这里需要优化查询太多了
func (self *SSecurityGroup) GetRules() ([]cloudprovider.SecurityRule, error) {
	rules := make([]cloudprovider.SecurityRule, 0)
	for _, r := range self.SecurityGroupRules {
		if !compatibleSecurityGroupRule(r) {
			continue
		}

		rule, err := self.GetSecurityRule(r.ID)
		if err != nil {
			return rules, err
		}

		rules = append(rules, rule)
	}

	return rules, nil
}

func (self *SSecurityGroup) GetSecurityRule(ruleId string) (cloudprovider.SecurityRule, error) {
	remoteRule := SecurityGroupRuleDetail{}
	err := DoGet(self.region.ecsClient.SecurityGroupRules.Get, ruleId, nil, &remoteRule)
	if err != nil {
		return cloudprovider.SecurityRule{}, err
	}

	var direction secrules.TSecurityRuleDirection
	if remoteRule.Direction == "ingress" {
		direction = secrules.SecurityRuleIngress
	} else {
		direction = secrules.SecurityRuleEgress
	}

	protocol := secrules.PROTO_ANY
	if remoteRule.Protocol != "" {
		protocol = remoteRule.Protocol
	}

	var portStart int
	var portEnd int
	if protocol == secrules.PROTO_ICMP {
		portStart = -1
		portEnd = -1
	} else {
		portStart = int(remoteRule.PortRangeMin)
		portEnd = int(remoteRule.PortRangeMax)
	}

	ipNet := &net.IPNet{}
	if len(remoteRule.RemoteIPPrefix) > 0 {
		_, ipNet, err = net.ParseCIDR(remoteRule.RemoteIPPrefix)
	} else {
		_, ipNet, err = net.ParseCIDR("0.0.0.0/0")
	}

	if err != nil {
		return cloudprovider.SecurityRule{}, err
	}

	rule := cloudprovider.SecurityRule{
		ExternalId: ruleId,
		SecurityRule: secrules.SecurityRule{
			Priority:    1,
			Action:      secrules.SecurityRuleAllow,
			IPNet:       ipNet,
			Protocol:    protocol,
			Direction:   direction,
			PortStart:   portStart,
			PortEnd:     portEnd,
			Ports:       nil,
			Description: remoteRule.Description,
		},
	}

	err = rule.ValidateRule()
	return rule, err
}

func (self *SRegion) GetSecurityGroupDetails(secGroupId string) (*SSecurityGroup, error) {
	securitygroup := SSecurityGroup{}
	err := DoGet(self.ecsClient.SecurityGroups.Get, secGroupId, nil, &securitygroup)
	if err != nil {
		return nil, err
	}

	securitygroup.region = self
	return &securitygroup, err
}

// https://support.huaweicloud.com/api-vpc/zh-cn_topic_0020090617.html
func (self *SRegion) GetSecurityGroups(vpcId string, name string) ([]SSecurityGroup, error) {
	querys := map[string]string{}
	if len(vpcId) > 0 && !utils.IsInStringArray(vpcId, []string{"default", api.NORMAL_VPC_ID}) { // vpc_id = default or normal 时报错 '{"code":"VPC.0601","message":"Query security groups error vpcId is invalid."}'
		querys["vpc_id"] = vpcId
	}

	securitygroups := make([]SSecurityGroup, 0)
	err := doListAllWithMarker(self.ecsClient.SecurityGroups.List, querys, &securitygroups)
	if err != nil {
		return nil, err
	}

	// security 中的vpc字段只是一个标识，实际可以跨vpc使用
	for i := range securitygroups {
		securitygroup := &securitygroups[i]
		securitygroup.region = self
	}

	result := []SSecurityGroup{}
	for _, secgroup := range securitygroups {
		if len(name) == 0 || secgroup.Name == name {
			result = append(result, secgroup)
		}
	}

	return result, nil
}

func (self *SSecurityGroup) GetProjectId() string {
	return ""
}

func (self *SSecurityGroup) Delete() error {
	return self.region.DeleteSecurityGroup(self.ID)
}

func (self *SSecurityGroup) SyncRules(common, inAdds, outAdds, inDels, outDels []cloudprovider.SecurityRule) error {
	for _, r := range append(inDels, outDels...) {
		err := self.region.delSecurityGroupRule(r.ExternalId)
		if err != nil {
			return errors.Wrapf(err, "delSecurityGroupRule(%s %s)", r.ExternalId, r.String())
		}
	}
	for _, r := range append(inAdds, outAdds...) {
		err := self.region.addSecurityGroupRules(self.ID, r)
		if err != nil {
			return errors.Wrapf(err, "addSecurityGroupRule(%d %s)", r.Priority, r.String())
		}
	}
	return nil
}
