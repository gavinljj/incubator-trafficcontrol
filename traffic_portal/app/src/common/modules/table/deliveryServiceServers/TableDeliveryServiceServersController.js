/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

var TableDeliveryServiceServersController = function(deliveryService, servers, $scope, $state, $uibModal, locationUtils, serverUtils, deliveryServiceService, propertiesModel) {

	var removeServer = function(serverId) {
		deliveryServiceService.deleteDeliveryServiceServer($scope.deliveryService.id, serverId)
			.then(
				function() {
					$scope.refresh();
				}
			);
	};

	$scope.deliveryService = deliveryService;

	$scope.servers = servers;

	$scope.editServer = function(id) {
		locationUtils.navigateToPath('/servers/' + id);
	};

	$scope.refresh = function() {
		$state.reload(); // reloads all the resolves for the view
	};

	$scope.selectServers = function() {
		var modalInstance = $uibModal.open({
			templateUrl: 'common/modules/table/deliveryServiceServers/table.assignDSServers.tpl.html',
			controller: 'TableAssignDSServersController',
			size: 'lg',
			resolve: {
				deliveryService: function() {
					return deliveryService;
				},
				servers: function(serverService) {
					return serverService.getEligibleDeliveryServiceServers(deliveryService.id);
				},
				assignedServers: function() {
					return servers;
				}
			}
		});
		modalInstance.result.then(function(selectedServerIds) {
			deliveryServiceService.assignDeliveryServiceServers(deliveryService.id, selectedServerIds)
				.then(
					function() {
						$scope.refresh();
					}
				);
		}, function () {
			// do nothing
		});
	};

	$scope.confirmRemoveServer = function(server, $event) {
		$event.stopPropagation(); // this kills the click event so it doesn't trigger anything else

		var params = {
			title: 'Remove Server from Delivery Service?',
			message: 'Are you sure you want to remove ' + server.hostName + ' from this delivery service?'
		};
		var modalInstance = $uibModal.open({
			templateUrl: 'common/modules/dialog/confirm/dialog.confirm.tpl.html',
			controller: 'DialogConfirmController',
			size: 'md',
			resolve: {
				params: function () {
					return params;
				}
			}
		});
		modalInstance.result.then(function() {
			removeServer(server.id);
		}, function () {
			// do nothing
		});
	};

	$scope.showChartsButton = propertiesModel.properties.servers.charts.show;

	$scope.ssh = serverUtils.ssh;

	$scope.openCharts = serverUtils.openCharts;

	$scope.isOffline = serverUtils.isOffline;

	$scope.offlineReason = serverUtils.offlineReason;

	$scope.navigateToPath = locationUtils.navigateToPath;

	angular.element(document).ready(function () {
		$('#serversTable').dataTable({
			"aLengthMenu": [[25, 50, 100, -1], [25, 50, 100, "All"]],
			"iDisplayLength": 25,
			"columnDefs": [
				{ 'orderable': false, 'targets': 11 }
			],
			"aaSorting": []
		});
	});

};

TableDeliveryServiceServersController.$inject = ['deliveryService', 'servers', '$scope', '$state', '$uibModal', 'locationUtils', 'serverUtils', 'deliveryServiceService', 'propertiesModel'];
module.exports = TableDeliveryServiceServersController;
