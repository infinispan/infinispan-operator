package operatorconfig

const dashboardJSON = `{
	"__inputs": [
	  {
		"name": "DS_PROMETHEUS",
		"label": "Prometheus",
		"description": "",
		"type": "datasource",
		"pluginId": "prometheus",
		"pluginName": "Prometheus"
	  }
	],
	"__requires": [
      {
        "type": "grafana",
        "id": "grafana",
        "name": "Grafana",
        "version": "6.2.1"
      },
      {
        "type": "panel",
        "id": "graph",
        "name": "Graph",
        "version": ""
      },
      {
        "type": "datasource",
        "id": "prometheus",
        "name": "Prometheus",
        "version": "1.0.0"
      },
      {
        "type": "panel",
        "name": "Singlestat",
        "version": ""
      }
    ],
		"annotations": {
		  "list": [
			{
			  "builtIn": 1,
			  "datasource": "-- Grafana --",
			  "enable": true,
			  "hide": true,
			  "iconColor": "rgba(0, 211, 255, 1)",
			  "name": "Annotations & Alerts",
			  "type": "dashboard"
			}
		  ]
		},
		"editable": true,
		"gnetId": null,
		"graphTooltip": 0,
		"id": 1,
		"iteration": 1610657246634,
		"links": [],
		"panels": [
		  {
			"collapsed": false,
			"datasource": null,
			"gridPos": {
			  "h": 1,
			  "w": 24,
			  "x": 0,
			  "y": 0
			},
			"id": 33,
			"panels": [],
			"title": "Summary",
			"type": "row"
		  },
		  {
			"cacheTimeout": null,
			"colorBackground": false,
			"colorValue": false,
			"colors": [
			  "#299c46",
			  "rgba(237, 129, 40, 0.89)",
			  "#d44a3a"
			],
			"datasource": null,
			"description": "The number of pods in the cluster",
			"fieldConfig": {
			  "defaults": {
				"custom": {}
			  },
			  "overrides": []
			},
			"format": "none",
			"gauge": {
			  "maxValue": 100,
			  "minValue": 0,
			  "show": false,
			  "thresholdLabels": false,
			  "thresholdMarkers": true
			},
			"gridPos": {
			  "h": 6,
			  "w": 2,
			  "x": 0,
			  "y": 1
			},
			"id": 52,
			"interval": null,
			"links": [],
			"mappingType": 1,
			"mappingTypes": [
			  {
				"name": "value to text",
				"value": 1
			  },
			  {
				"name": "range to text",
				"value": 2
			  }
			],
			"maxDataPoints": 100,
			"nullPointMode": "connected",
			"nullText": null,
			"pluginVersion": "6.2.4",
			"postfix": "",
			"postfixFontSize": "50%",
			"prefix": "",
			"prefixFontSize": "50%",
			"rangeMaps": [
			  {
				"from": "null",
				"text": "N/A",
				"to": "null"
			  }
			],
			"sparkline": {
			  "fillColor": "rgba(31, 118, 189, 0.18)",
			  "full": false,
			  "lineColor": "rgb(31, 120, 193)",
			  "show": false
			},
			"tableColumn": "",
			"targets": [
			  {
				"expr": "sum(kube_pod_status_ready{namespace=~\"$namespace\",pod !~ \"infinispan-operator.*\"}) by (namespace) # kube specific, need OS?",
				"format": "time_series",
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "Replicas",
				"refId": "A"
			  }
			],
			"thresholds": "",
			"timeFrom": null,
			"timeShift": null,
			"title": "Replicas (up)",
			"type": "singlestat",
			"valueFontSize": "80%",
			"valueMaps": [
			  {
				"op": "=",
				"text": "N/A",
				"value": "null"
			  }
			],
			"valueName": "avg"
		  },
		  {
			"aliasColors": {},
			"bars": false,
			"dashLength": 10,
			"dashes": false,
			"datasource": null,
			"fieldConfig": {
			  "defaults": {
				"custom": {}
			  },
			  "overrides": []
			},
			"fill": 1,
			"fillGradient": 0,
			"gridPos": {
			  "h": 6,
			  "w": 5,
			  "x": 2,
			  "y": 1
			},
			"hiddenSeries": false,
			"id": 25,
			"legend": {
			  "avg": false,
			  "current": false,
			  "max": false,
			  "min": false,
			  "show": true,
			  "total": false,
			  "values": false
			},
			"lines": true,
			"linewidth": 1,
			"links": [],
			"nullPointMode": "null as zero",
			"percentage": false,
			"pluginVersion": "7.1.1",
			"pointradius": 2,
			"points": false,
			"renderer": "flot",
			"seriesOverrides": [],
			"spaceLength": 10,
			"stack": false,
			"steppedLine": false,
			"targets": [
			  {
				"expr": "sum(up{namespace=~\"$namespace\", job=~\"$cluster\"}) by (pod)",
				"format": "time_series",
				"hide": false,
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "{{pod}}",
				"refId": "A"
			  },
			  {
				"expr": "sum(up{namespace=~\"$namespace\", job=~\"infinispan-operator.*\"}) by (pod) # TODO - what does the operator expose?",
				"format": "time_series",
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "{{pod}}",
				"refId": "B"
			  }
			],
			"thresholds": [],
			"timeFrom": null,
			"timeRegions": [],
			"timeShift": null,
			"title": "Readiness Probes",
			"tooltip": {
			  "shared": true,
			  "sort": 0,
			  "value_type": "individual"
			},
			"type": "graph",
			"xaxis": {
			  "buckets": null,
			  "mode": "time",
			  "name": null,
			  "show": true,
			  "values": []
			},
			"yaxes": [
			  {
				"decimals": 0,
				"format": "short",
				"label": "Ready",
				"logBase": 1,
				"max": "1",
				"min": "0",
				"show": true
			  },
			  {
				"format": "short",
				"label": null,
				"logBase": 1,
				"max": null,
				"min": null,
				"show": false
			  }
			],
			"yaxis": {
			  "align": false,
			  "alignLevel": null
			}
		  },
		  {
			"cacheTimeout": null,
			"datasource": "Prometheus",
			"fieldConfig": {
			  "defaults": {
				"custom": {},
				"mappings": [
				  {
					"id": 0,
					"op": "=",
					"text": "N/A",
					"type": 1,
					"value": "null"
				  }
				],
				"max": 100,
				"min": 0,
				"nullValueMode": "connected",
				"thresholds": {
				  "mode": "absolute",
				  "steps": [
					{
					  "color": "#299c46",
					  "value": null
					},
					{
					  "color": "rgba(237, 129, 40, 0.89)",
					  "value": 80
					},
					{
					  "color": "#d44a3a",
					  "value": 90
					}
				  ]
				},
				"unit": "percent"
			  },
			  "overrides": []
			},
			"gridPos": {
			  "h": 6,
			  "w": 5,
			  "x": 7,
			  "y": 1
			},
			"id": 56,
			"interval": null,
			"links": [],
			"maxDataPoints": 100,
			"options": {
			  "orientation": "horizontal",
			  "reduceOptions": {
				"calcs": [
				  "lastNotNull"
				],
				"fields": "",
				"values": false
			  },
			  "showThresholdLabels": false,
			  "showThresholdMarkers": true
			},
			"pluginVersion": "7.1.1",
			"repeatDirection": "h",
			"targets": [
			  {
				"expr": "sum(base_memory_usedNonHeap_bytes{namespace=~\"$namespace\", job=~\"$cluster\"})*100/sum(base_memory_maxNonHeap_bytes{namespace=~\"$namespace\", job=~\"$cluster\"})",
				"format": "time_series",
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "",
				"refId": "A"
			  }
			],
			"timeFrom": null,
			"timeShift": null,
			"title": "Memory Off-Heap",
			"type": "gauge"
		  },
		  {
			"cacheTimeout": null,
			"datasource": "Prometheus",
			"fieldConfig": {
			  "defaults": {
				"custom": {},
				"mappings": [
				  {
					"id": 0,
					"op": "=",
					"text": "N/A",
					"type": 1,
					"value": "null"
				  }
				],
				"max": 100,
				"min": 0,
				"nullValueMode": "connected",
				"thresholds": {
				  "mode": "absolute",
				  "steps": [
					{
					  "color": "#299c46",
					  "value": null
					},
					{
					  "color": "rgba(237, 129, 40, 0.89)",
					  "value": 80
					},
					{
					  "color": "#d44a3a",
					  "value": 90
					}
				  ]
				},
				"unit": "percent"
			  },
			  "overrides": []
			},
			"gridPos": {
			  "h": 6,
			  "w": 5,
			  "x": 12,
			  "y": 1
			},
			"id": 54,
			"interval": null,
			"links": [],
			"maxDataPoints": 100,
			"maxPerRow": 4,
			"options": {
			  "orientation": "horizontal",
			  "reduceOptions": {
				"calcs": [
				  "lastNotNull"
				],
				"fields": "",
				"values": false
			  },
			  "showThresholdLabels": false,
			  "showThresholdMarkers": true
			},
			"pluginVersion": "7.1.1",
			"repeat": null,
			"repeatDirection": "h",
			"targets": [
			  {
				"expr": "sum(base_memory_usedHeap_bytes{namespace=~\"$namespace\", job=~\"$cluster\"})*100/sum(base_memory_maxHeap_bytes{namespace=~\"$namespace\", job=~\"$cluster\"})",
				"format": "time_series",
				"hide": false,
				"instant": false,
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "",
				"refId": "A"
			  }
			],
			"timeFrom": null,
			"timeShift": null,
			"title": "Memory Heap",
			"type": "gauge"
		  },
		  {
			"collapsed": false,
			"datasource": null,
			"gridPos": {
			  "h": 1,
			  "w": 24,
			  "x": 0,
			  "y": 7
			},
			"id": 35,
			"panels": [],
			"title": "Resource Metrics",
			"type": "row"
		  },
		  {
			"aliasColors": {},
			"bars": false,
			"dashLength": 10,
			"dashes": false,
			"datasource": "Prometheus",
			"fieldConfig": {
			  "defaults": {
				"custom": {}
			  },
			  "overrides": []
			},
			"fill": 1,
			"fillGradient": 0,
			"gridPos": {
			  "h": 6,
			  "w": 7,
			  "x": 0,
			  "y": 8
			},
			"hiddenSeries": false,
			"hideTimeOverride": false,
			"id": 12,
			"legend": {
			  "alignAsTable": false,
			  "avg": true,
			  "current": true,
			  "hideEmpty": true,
			  "hideZero": true,
			  "max": true,
			  "min": true,
			  "rightSide": false,
			  "show": true,
			  "sideWidth": 70,
			  "total": false,
			  "values": true
			},
			"lines": true,
			"linewidth": 1,
			"links": [],
			"nullPointMode": "connected",
			"percentage": false,
			"pluginVersion": "7.1.1",
			"pointradius": 5,
			"points": false,
			"renderer": "flot",
			"seriesOverrides": [],
			"spaceLength": 10,
			"stack": false,
			"steppedLine": false,
			"targets": [
			  {
				"expr": "min(kube_pod_container_resource_requests{namespace=~\"$namespace\", resource=\"memory\", container=\"infinispan\"})\n",
				"format": "time_series",
				"hide": false,
				"instant": false,
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "Requests",
				"refId": "A"
			  },
			  {
				"expr": "min(kube_pod_container_resource_limits{namespace=~\"$namespace\", resource=\"memory\", container=\"infinispan\"})",
				"format": "time_series",
				"hide": false,
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "Limits",
				"refId": "C"
			  },
			  {
				"expr": "container_memory_rss{namespace=~\"$namespace\", container=\"infinispan\"}",
				"format": "time_series",
				"hide": false,
				"instant": false,
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "RSS for {{pod}}",
				"refId": "B"
			  },
			  {
				"expr": "base_memory_committedHeap_bytes{namespace=~\"$namespace\", job=~\"$cluster\"}",
				"format": "time_series",
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "JVM Committed for {{pod}}",
				"refId": "D"
			  },
			  {
				"expr": "base_memory_maxHeap_bytes{namespace=~\"$namespace\", job=~\"$cluster\"}",
				"format": "time_series",
				"hide": false,
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "JVM Max for {{pod}}",
				"refId": "E"
			  }
			],
			"thresholds": [],
			"timeFrom": null,
			"timeRegions": [],
			"timeShift": null,
			"title": "Pod Memory Usage",
			"tooltip": {
			  "shared": true,
			  "sort": 0,
			  "value_type": "individual"
			},
			"type": "graph",
			"xaxis": {
			  "buckets": null,
			  "mode": "time",
			  "name": null,
			  "show": true,
			  "values": []
			},
			"yaxes": [
			  {
				"decimals": null,
				"format": "bytes",
				"label": "",
				"logBase": 1,
				"max": null,
				"min": "0",
				"show": true
			  },
			  {
				"format": "short",
				"label": null,
				"logBase": 2,
				"max": null,
				"min": null,
				"show": false
			  }
			],
			"yaxis": {
			  "align": false,
			  "alignLevel": null
			}
		  },
		  {
			"aliasColors": {},
			"bars": false,
			"dashLength": 10,
			"dashes": false,
			"datasource": null,
			"fieldConfig": {
			  "defaults": {
				"custom": {}
			  },
			  "overrides": []
			},
			"fill": 1,
			"fillGradient": 0,
			"gridPos": {
			  "h": 6,
			  "w": 5,
			  "x": 7,
			  "y": 8
			},
			"hiddenSeries": false,
			"id": 31,
			"legend": {
			  "avg": false,
			  "current": false,
			  "max": false,
			  "min": false,
			  "show": true,
			  "total": false,
			  "values": false
			},
			"lines": true,
			"linewidth": 1,
			"links": [],
			"nullPointMode": "null",
			"percentage": false,
			"pluginVersion": "7.1.1",
			"pointradius": 2,
			"points": false,
			"renderer": "flot",
			"seriesOverrides": [],
			"spaceLength": 10,
			"stack": false,
			"steppedLine": false,
			"targets": [
			  {
				"expr": "kube_pod_container_resource_limits{namespace=~\"$namespace\", resource=\"cpu\", job=~\"$cluster\"}",
				"format": "time_series",
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "CPU Limit for {{pod}}",
				"refId": "A"
			  },
			  {
				"expr": "kube_pod_container_resource_requests{namespace=~\"$namespace\", resource=\"cpu\", job=~\"$cluster\"}",
				"format": "time_series",
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "CPU Request for {{pod}}",
				"refId": "C"
			  },
			  {
				"expr": "base_cpu_systemLoadAverage{namespace=~\"$namespace\", job=~\"$cluster\"}",
				"format": "time_series",
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "CPU Load {{pod}}",
				"refId": "B"
			  }
			],
			"thresholds": [],
			"timeFrom": null,
			"timeRegions": [],
			"timeShift": null,
			"title": "CPU Load",
			"tooltip": {
			  "shared": true,
			  "sort": 0,
			  "value_type": "individual"
			},
			"type": "graph",
			"xaxis": {
			  "buckets": null,
			  "mode": "time",
			  "name": null,
			  "show": true,
			  "values": []
			},
			"yaxes": [
			  {
				"format": "short",
				"label": null,
				"logBase": 1,
				"max": null,
				"min": null,
				"show": true
			  },
			  {
				"format": "short",
				"label": null,
				"logBase": 1,
				"max": null,
				"min": null,
				"show": true
			  }
			],
			"yaxis": {
			  "align": false,
			  "alignLevel": null
			}
		  },
		  {
			"aliasColors": {},
			"bars": false,
			"cacheTimeout": null,
			"dashLength": 10,
			"dashes": false,
			"datasource": null,
			"fieldConfig": {
			  "defaults": {
				"custom": {}
			  },
			  "overrides": []
			},
			"fill": 1,
			"fillGradient": 0,
			"gridPos": {
			  "h": 6,
			  "w": 5,
			  "x": 12,
			  "y": 8
			},
			"hiddenSeries": false,
			"id": 37,
			"legend": {
			  "avg": false,
			  "current": false,
			  "max": false,
			  "min": false,
			  "show": true,
			  "total": false,
			  "values": false
			},
			"lines": true,
			"linewidth": 1,
			"links": [],
			"nullPointMode": "null as zero",
			"percentage": false,
			"pluginVersion": "7.1.1",
			"pointradius": 2,
			"points": false,
			"renderer": "flot",
			"seriesOverrides": [],
			"spaceLength": 10,
			"stack": false,
			"steppedLine": false,
			"targets": [
			  {
				"expr": "sum(increase(base_gc_total{namespace=~\"$namespace\", job=~\"$cluster\"}[30m])) by (pod)",
				"format": "time_series",
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "GC Collections for {{pod}}",
				"refId": "A"
			  }
			],
			"thresholds": [],
			"timeFrom": null,
			"timeRegions": [],
			"timeShift": null,
			"title": "Number of GC Collections [30m]",
			"tooltip": {
			  "shared": true,
			  "sort": 0,
			  "value_type": "individual"
			},
			"type": "graph",
			"xaxis": {
			  "buckets": null,
			  "mode": "time",
			  "name": null,
			  "show": true,
			  "values": []
			},
			"yaxes": [
			  {
				"format": "short",
				"label": null,
				"logBase": 1,
				"max": null,
				"min": null,
				"show": true
			  },
			  {
				"format": "short",
				"label": null,
				"logBase": 1,
				"max": null,
				"min": null,
				"show": true
			  }
			],
			"yaxis": {
			  "align": false,
			  "alignLevel": null
			}
		  },
		  {
			"aliasColors": {},
			"bars": false,
			"cacheTimeout": null,
			"dashLength": 10,
			"dashes": false,
			"datasource": null,
			"fieldConfig": {
			  "defaults": {
				"custom": {}
			  },
			  "overrides": []
			},
			"fill": 1,
			"fillGradient": 0,
			"gridPos": {
			  "h": 6,
			  "w": 7,
			  "x": 17,
			  "y": 8
			},
			"hiddenSeries": false,
			"id": 50,
			"legend": {
			  "avg": false,
			  "current": false,
			  "max": false,
			  "min": false,
			  "show": true,
			  "total": false,
			  "values": false
			},
			"lines": true,
			"linewidth": 1,
			"links": [],
			"nullPointMode": "null",
			"percentage": false,
			"pluginVersion": "7.1.1",
			"pointradius": 2,
			"points": false,
			"renderer": "flot",
			"seriesOverrides": [],
			"spaceLength": 10,
			"stack": false,
			"steppedLine": false,
			"targets": [
			  {
				"expr": "base_thread_count{namespace=~\"$namespace\",job=~\"$cluster\"}",
				"format": "time_series",
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "Threads {{pod}}",
				"refId": "A"
			  },
			  {
				"expr": "base_thread_daemon_count{namespace=~\"$namespace\",job=~\"$cluster\"}",
				"format": "time_series",
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "Deamon Threads  {{pod}}",
				"refId": "B"
			  },
			  {
				"expr": "base_thread_max_count{namespace=~\"$namespace\",job=~\"$cluster\"}",
				"format": "time_series",
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "Max Threads {{pod}}",
				"refId": "C"
			  }
			],
			"thresholds": [],
			"timeFrom": null,
			"timeRegions": [],
			"timeShift": null,
			"title": "IO Threads",
			"tooltip": {
			  "shared": true,
			  "sort": 0,
			  "value_type": "individual"
			},
			"type": "graph",
			"xaxis": {
			  "buckets": null,
			  "mode": "time",
			  "name": null,
			  "show": true,
			  "values": []
			},
			"yaxes": [
			  {
				"format": "short",
				"label": null,
				"logBase": 1,
				"max": null,
				"min": null,
				"show": true
			  },
			  {
				"format": "short",
				"label": null,
				"logBase": 1,
				"max": null,
				"min": null,
				"show": true
			  }
			],
			"yaxis": {
			  "align": false,
			  "alignLevel": null
			}
		  },
		  {
			"aliasColors": {},
			"bars": false,
			"dashLength": 10,
			"dashes": false,
			"datasource": "Prometheus",
			"fieldConfig": {
			  "defaults": {
				"custom": {}
			  },
			  "overrides": []
			},
			"fill": 1,
			"fillGradient": 0,
			"gridPos": {
			  "h": 6,
			  "w": 7,
			  "x": 0,
			  "y": 14
			},
			"hiddenSeries": false,
			"hideTimeOverride": false,
			"id": 39,
			"legend": {
			  "alignAsTable": false,
			  "avg": true,
			  "current": true,
			  "hideEmpty": true,
			  "hideZero": true,
			  "max": true,
			  "min": true,
			  "rightSide": false,
			  "show": true,
			  "sideWidth": 70,
			  "total": false,
			  "values": true
			},
			"lines": true,
			"linewidth": 1,
			"links": [],
			"nullPointMode": "connected",
			"percentage": false,
			"pluginVersion": "7.1.1",
			"pointradius": 5,
			"points": false,
			"renderer": "flot",
			"seriesOverrides": [],
			"spaceLength": 10,
			"stack": false,
			"steppedLine": false,
			"targets": [
			  {
				"expr": "kube_pod_container_resource_requests{namespace=\"$namespace\", resource=\"memory\"}",
				"format": "time_series",
				"hide": false,
				"instant": false,
				"intervalFactor": 1,
				"legendFormat": "Requests",
				"refId": "A"
			  },
			  {
				"expr": "kube_pod_container_resource_limits{namespace=\"$namespace\", resource=\"memory\"}",
				"format": "time_series",
				"intervalFactor": 1,
				"legendFormat": "Limits",
				"refId": "C"
			  },
			  {
				"expr": "sum(base_memory_usedHeap_bytes{namespace=\"$namespace\"}) by (pod)",
				"format": "time_series",
				"intervalFactor": 1,
				"legendFormat": "JVM Heap {{pod}}",
				"refId": "E"
			  },
			  {
				"expr": "sum(base_memory_usedNonHeap_bytes{namespace=\"$namespace\"}) by (pod)",
				"format": "time_series",
				"intervalFactor": 1,
				"legendFormat": "JVM Non Heap {{pod}}",
				"refId": "B"
			  }
			],
			"thresholds": [],
			"timeFrom": null,
			"timeRegions": [],
			"timeShift": null,
			"title": "JVM Memory Usage",
			"tooltip": {
			  "shared": true,
			  "sort": 0,
			  "value_type": "individual"
			},
			"type": "graph",
			"xaxis": {
			  "buckets": null,
			  "mode": "time",
			  "name": null,
			  "show": true,
			  "values": []
			},
			"yaxes": [
			  {
				"decimals": null,
				"format": "bytes",
				"label": "",
				"logBase": 1,
				"max": null,
				"min": "0",
				"show": true
			  },
			  {
				"format": "short",
				"label": null,
				"logBase": 2,
				"max": null,
				"min": null,
				"show": false
			  }
			],
			"yaxis": {
			  "align": false,
			  "alignLevel": null
			}
		  },
		  {
			"aliasColors": {},
			"bars": false,
			"cacheTimeout": null,
			"dashLength": 10,
			"dashes": false,
			"datasource": null,
			"fieldConfig": {
			  "defaults": {
				"custom": {}
			  },
			  "overrides": []
			},
			"fill": 1,
			"fillGradient": 0,
			"gridPos": {
			  "h": 6,
			  "w": 5,
			  "x": 12,
			  "y": 14
			},
			"hiddenSeries": false,
			"id": 38,
			"legend": {
			  "avg": false,
			  "current": false,
			  "max": false,
			  "min": false,
			  "show": true,
			  "total": false,
			  "values": false
			},
			"lines": true,
			"linewidth": 1,
			"links": [],
			"nullPointMode": "null as zero",
			"percentage": false,
			"pluginVersion": "7.1.1",
			"pointradius": 2,
			"points": false,
			"renderer": "flot",
			"seriesOverrides": [],
			"spaceLength": 10,
			"stack": false,
			"steppedLine": false,
			"targets": [
			  {
				"expr": "sum(increase(base_gc_time_total_seconds{namespace=~\"$namespace\", job=~\"$cluster\"}[30m])) by (pod)",
				"format": "time_series",
				"interval": "",
				"intervalFactor": 1,
				"legendFormat": "Total GC Time for {{pod}}",
				"refId": "A"
			  }
			],
			"thresholds": [],
			"timeFrom": null,
			"timeRegions": [],
			"timeShift": null,
			"title": "Total GC Time [30m]",
			"tooltip": {
			  "shared": true,
			  "sort": 0,
			  "value_type": "individual"
			},
			"type": "graph",
			"xaxis": {
			  "buckets": null,
			  "mode": "time",
			  "name": null,
			  "show": true,
			  "values": []
			},
			"yaxes": [
			  {
				"format": "s",
				"label": null,
				"logBase": 1,
				"max": null,
				"min": null,
				"show": true
			  },
			  {
				"format": "short",
				"label": null,
				"logBase": 1,
				"max": null,
				"min": null,
				"show": true
			  }
			],
			"yaxis": {
			  "align": false,
			  "alignLevel": null
			}
		  },
		  {
			"collapsed": false,
			"datasource": null,
			"gridPos": {
			  "h": 1,
			  "w": 24,
			  "x": 0,
			  "y": 20
			},
			"id": 58,
			"panels": [],
			"repeat": null,
			"title": "Caches",
			"type": "row"
		  },
		  {
			"cacheTimeout": null,
			"datasource": "Prometheus",
			"fieldConfig": {
			  "defaults": {
				"color": {
				  "mode": "thresholds"
				},
				"custom": {
				  "align": null
				},
				"mappings": [],
				"max": 100,
				"min": 0,
				"thresholds": {
				  "mode": "absolute",
				  "steps": [
					{
					  "color": "green",
					  "index": 0,
					  "value": null
					},
					{
					  "color": "red",
					  "index": 1,
					  "value": 80
					}
				  ]
				}
			  },
			  "overrides": []
			},
			"gridPos": {
			  "h": 6,
			  "w": 6,
			  "x": 0,
			  "y": 21
			},
			"id": 60,
			"links": [],
			"maxPerRow": 4,
			"options": {
			  "displayMode": "gradient",
			  "orientation": "horizontal",
			  "reduceOptions": {
				"calcs": [
				  "last"
				],
				"fields": "",
				"values": false
			  },
			  "showUnfilled": true
			},
			"pluginVersion": "7.1.1",
			"repeat": "caches",
			"repeatDirection": "v",
			"scopedVars": {
			  "caches": {
				"selected": false,
				"text": "default",
				"value": "default"
			  }
			},
			"targets": [
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_number_of_entries\"})",
				"format": "time_series",
				"instant": false,
				"intervalFactor": 1,
				"legendFormat": "Entries",
				"refId": "A"
			  },
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_number_of_entries_in_memory\"})",
				"format": "time_series",
				"instant": false,
				"intervalFactor": 1,
				"legendFormat": "Entries In-Memory",
				"refId": "B"
			  },
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_hits\"})",
				"format": "time_series",
				"instant": false,
				"intervalFactor": 1,
				"legendFormat": "Hits",
				"refId": "C"
			  },
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_misses\"})",
				"format": "time_series",
				"instant": false,
				"intervalFactor": 1,
				"legendFormat": "Misses",
				"refId": "D"
			  },
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_remove_hits\"})",
				"format": "time_series",
				"instant": false,
				"intervalFactor": 1,
				"legendFormat": "Remove Hits",
				"refId": "E"
			  },
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_remove_misses\"})",
				"format": "time_series",
				"intervalFactor": 1,
				"legendFormat": "Remove Misses",
				"refId": "F"
			  }
			],
			"timeFrom": null,
			"timeShift": null,
			"title": "$caches",
			"type": "bargauge"
		  },
		  {
			"datasource": null,
			"description": "",
			"fieldConfig": {
			  "defaults": {
				"color": {
				  "mode": "thresholds"
				},
				"custom": {},
				"mappings": [],
				"max": 100,
				"min": 0,
				"thresholds": {
				  "mode": "absolute",
				  "steps": [
					{
					  "color": "green",
					  "index": 0,
					  "value": null
					},
					{
					  "color": "red",
					  "index": 1,
					  "value": 80
					}
				  ]
				},
				"unit": "ms"
			  },
			  "overrides": []
			},
			"gridPos": {
			  "h": 6,
			  "w": 6,
			  "x": 6,
			  "y": 21
			},
			"id": 116,
			"links": [],
			"maxPerRow": 4,
			"options": {
			  "displayMode": "gradient",
			  "orientation": "horizontal",
			  "reduceOptions": {
				"calcs": [
				  "mean"
				],
				"fields": "",
				"values": false
			  },
			  "showUnfilled": true
			},
			"pluginVersion": "7.1.1",
			"repeat": "caches",
			"repeatDirection": "v",
			"scopedVars": {
			  "caches": {
				"selected": false,
				"text": "default",
				"value": "default"
			  }
			},
			"targets": [
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_average_write_time\"})",
				"format": "time_series",
				"intervalFactor": 1,
				"legendFormat": "Writes",
				"refId": "A"
			  },
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_average_read_time\"})",
				"format": "time_series",
				"intervalFactor": 1,
				"legendFormat": "Reads",
				"refId": "B"
			  },
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_average_remove_time\"})",
				"format": "time_series",
				"intervalFactor": 1,
				"legendFormat": "Remove",
				"refId": "C"
			  }
			],
			"timeFrom": null,
			"timeShift": null,
			"title": "$caches Latencies",
			"type": "bargauge"
		  },
		  {
			"cacheTimeout": null,
			"datasource": "Prometheus",
			"fieldConfig": {
			  "defaults": {
				"color": {
				  "mode": "thresholds"
				},
				"custom": {
				  "align": null
				},
				"mappings": [],
				"max": 100,
				"min": 0,
				"thresholds": {
				  "mode": "absolute",
				  "steps": [
					{
					  "color": "green",
					  "index": 0,
					  "value": null
					},
					{
					  "color": "red",
					  "index": 1,
					  "value": 80
					}
				  ]
				}
			  },
			  "overrides": []
			},
			"gridPos": {
			  "h": 6,
			  "w": 6,
			  "x": 0,
			  "y": 27
			},
			"id": 117,
			"links": [],
			"maxPerRow": 4,
			"options": {
			  "displayMode": "gradient",
			  "orientation": "horizontal",
			  "reduceOptions": {
				"calcs": [
				  "last"
				],
				"fields": "",
				"values": false
			  },
			  "showUnfilled": true
			},
			"pluginVersion": "7.1.1",
			"repeat": null,
			"repeatDirection": "v",
			"repeatIteration": 1610657246634,
			"repeatPanelId": 60,
			"scopedVars": {
			  "caches": {
				"selected": false,
				"text": "fruits",
				"value": "fruits"
			  }
			},
			"targets": [
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_number_of_entries\"})",
				"format": "time_series",
				"instant": false,
				"intervalFactor": 1,
				"legendFormat": "Entries",
				"refId": "A"
			  },
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_number_of_entries_in_memory\"})",
				"format": "time_series",
				"instant": false,
				"intervalFactor": 1,
				"legendFormat": "Entries In-Memory",
				"refId": "B"
			  },
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_hits\"})",
				"format": "time_series",
				"instant": false,
				"intervalFactor": 1,
				"legendFormat": "Hits",
				"refId": "C"
			  },
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_misses\"})",
				"format": "time_series",
				"instant": false,
				"intervalFactor": 1,
				"legendFormat": "Misses",
				"refId": "D"
			  },
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_remove_hits\"})",
				"format": "time_series",
				"instant": false,
				"intervalFactor": 1,
				"legendFormat": "Remove Hits",
				"refId": "E"
			  },
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_remove_misses\"})",
				"format": "time_series",
				"intervalFactor": 1,
				"legendFormat": "Remove Misses",
				"refId": "F"
			  }
			],
			"timeFrom": null,
			"timeShift": null,
			"title": "$caches",
			"type": "bargauge"
		  },
		  {
			"datasource": null,
			"description": "",
			"fieldConfig": {
			  "defaults": {
				"color": {
				  "mode": "thresholds"
				},
				"custom": {},
				"mappings": [],
				"max": 100,
				"min": 0,
				"thresholds": {
				  "mode": "absolute",
				  "steps": [
					{
					  "color": "green",
					  "index": 0,
					  "value": null
					},
					{
					  "color": "red",
					  "index": 1,
					  "value": 80
					}
				  ]
				},
				"unit": "ms"
			  },
			  "overrides": []
			},
			"gridPos": {
			  "h": 6,
			  "w": 6,
			  "x": 6,
			  "y": 27
			},
			"id": 118,
			"links": [],
			"maxPerRow": 4,
			"options": {
			  "displayMode": "gradient",
			  "orientation": "horizontal",
			  "reduceOptions": {
				"calcs": [
				  "mean"
				],
				"fields": "",
				"values": false
			  },
			  "showUnfilled": true
			},
			"pluginVersion": "7.1.1",
			"repeat": null,
			"repeatDirection": "v",
			"repeatIteration": 1610657246634,
			"repeatPanelId": 116,
			"scopedVars": {
			  "caches": {
				"selected": false,
				"text": "fruits",
				"value": "fruits"
			  }
			},
			"targets": [
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_average_write_time\"})",
				"format": "time_series",
				"intervalFactor": 1,
				"legendFormat": "Writes",
				"refId": "A"
			  },
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_average_read_time\"})",
				"format": "time_series",
				"intervalFactor": 1,
				"legendFormat": "Reads",
				"refId": "B"
			  },
			  {
				"expr": "max({__name__=~\"vendor_cache_manager_default_cache_($caches)_cluster_cache_stats_average_remove_time\"})",
				"format": "time_series",
				"intervalFactor": 1,
				"legendFormat": "Remove",
				"refId": "C"
			  }
			],
			"timeFrom": null,
			"timeShift": null,
			"title": "$caches Latencies",
			"type": "bargauge"
		  }
		],
		"refresh": false,
		"schemaVersion": 26,
		"style": "dark",
		"tags": [],
		"templating": {
		  "list": [
			{
			  "allValue": null,
			  "current": {
				"selected": false,
				"text": "infinispan",
				"value": [
				  "infinispan"
				]
			  },
			  "datasource": "Prometheus",
			  "definition": "label_values(vendor_cache_manager_default_number_of_cache_configurations,namespace)",
			  "hide": 0,
			  "includeAll": true,
			  "label": "Namespace",
			  "multi": true,
			  "name": "namespace",
			  "options": [],
			  "query": "label_values(vendor_cache_manager_default_number_of_cache_configurations,namespace)",
			  "refresh": 1,
			  "regex": "",
			  "skipUrlSync": false,
			  "sort": 0,
			  "tagValuesQuery": "",
			  "tags": [],
			  "tagsQuery": "",
			  "type": "query",
			  "useTags": false
			},
			{
			  "allValue": null,
			  "current": {
				"selected": true,
				"tags": [],
				"text": "All",
				"value": [
				  "$__all"
				]
			  },
			  "datasource": "Prometheus",
			  "definition": "label_values(vendor_cache_manager_default_number_of_cache_configurations{namespace=~\"$namespace\"}, service)",
			  "hide": 0,
			  "includeAll": true,
			  "label": "Cluster",
			  "multi": true,
			  "name": "cluster",
			  "options": [],
			  "query": "label_values(vendor_cache_manager_default_number_of_cache_configurations{namespace=~\"$namespace\"}, service)",
			  "refresh": 1,
			  "regex": "",
			  "skipUrlSync": false,
			  "sort": 0,
			  "tagValuesQuery": "",
			  "tags": [],
			  "tagsQuery": "",
			  "type": "query",
			  "useTags": false
			},
			{
			  "allValue": null,
			  "current": {
				"selected": true,
				"tags": [],
				"text": "All",
				"value": [
				  "$__all"
				]
			  },
			  "datasource": "Prometheus",
			  "definition": "query_result({job=~\"$cluster\", namespace=~\"$namespace\", __name__=~\"vendor_cache_manager_default_cache_.*_cluster_cache_stats_activations\"})",
			  "hide": 0,
			  "includeAll": true,
			  "label": null,
			  "multi": true,
			  "name": "caches",
			  "options": [],
			  "query": "query_result({job=~\"$cluster\", namespace=~\"$namespace\", __name__=~\"vendor_cache_manager_default_cache_.*_cluster_cache_stats_activations\"})",
			  "refresh": 1,
			  "regex": "/vendor_cache_manager_default_cache_(.*)_cluster_cache_stats_activations/",
			  "skipUrlSync": false,
			  "sort": 0,
			  "tagValuesQuery": "",
			  "tags": [],
			  "tagsQuery": "",
			  "type": "query",
			  "useTags": false
			}
		  ]
		},
		"time": {
		  "from": "now-30m",
		  "to": "now"
		},
		"timepicker": {
		  "refresh_intervals": [
			"10s",
			"30s",
			"1m",
			"5m",
			"15m",
			"30m",
			"1h",
			"2h",
			"1d"
		  ],
		  "time_options": [
			"5m",
			"15m",
			"1h",
			"6h",
			"12h",
			"24h",
			"2d",
			"7d",
			"30d"
		  ]
		},
		"timezone": "",
		"title": "Infinispan",
		"uid": "U826WzBGz",
		"version": 35
	  }
`
