# Overview

The Volcano descheduler is developed based on the upstream Kubernetes community [descheduler](https://github.com/kubernetes-sigs/descheduler.git) project. Its initial version is based on the v0.27.1 tag of the upstream Kubernetes community, and the main codes come from the upstream descheduler project. The project also follows the Apache 2.0 open source license and retains the original license statement in the source file. In addition, Volcano descheduler also clearly marked its changes to the upstream descheduler project.

> This is an alpha version and code is subject to change.

# Why Volcano descheduler?

The upstream Kubernetes community's descheduler provides basic descheduling plugins and functions, but there are still the following problems that cannot meet the user's scenarios:

- When deploying descheduler in the form of Deployment, cronTab cannot be set to execute rescheduling tasks regularly. The native descheduler supports multiple deployment types, such as Job, cronJob, and Deployment. Users need to deploy multiple types of workloads to meet the needs of different scenarios.

- The descheduler makes descheduling decisions based on the resource requests of the Pod, without considering the actual load of the node, and there is a problem of inaccurate descheduling decisions. It is worth noting that descheduling is a relatively dangerous and destructive action. The timing and accuracy of rescheduling need to be strictly guaranteed to avoid unexpected behavior.

- It is hard to perfectly cooperate with the scheduler. Scheduling and descheduling are two mutually coordinated processes. When descheduling and evicting Pods, it is necessary to perceive whether the cluster can accommodate the newly generated Pods to avoid meaningless rescheduling, which is crucial to ensure business stability.

# Features

Volcano descheduler provides the following enhanced capabilities while fully retaining the functions and compatible code framework of the upstream Kubernetes community descheduler:

## Descheduling via crontab or fixed interval

Users can deploy the  `Volcano descheduler` as a Deploment type workload instead of a cronJob. Then specify the command line parameters to run the descheduler according to cronTab expression or fixed interval.

**cronTab scheduled task**: Specify the parameter `--descheduling-interval-cron-expression='0 0 * * *'`, which means to run descheduling once every morning.

**Fixed interval**: Specify the parameter `--descheduling-interval=10m`, which means descheduling will be run every 10 minutes.

And please notice that `--descheduling-interval` has a higher priority than `--descheduling-interval-cron-expression`, the descheduler's behavior is subject to the `--descheduling-interva` setting when both parameters are set.

## Real Load Aware Descheduling

In the process of kubernetes cluster governance, hotspots are often formed due to high CPU, memory and other utilization conditions, which not only affects the stable operation of Pods on the current node, but also leads to a surge in the chances of node failure. In order to cope with problems such as load imbalance of cluster nodes and dynamically balance the resource utilization rate among nodes, it is necessary to construct a cluster resource view based on the relevant monitoring metrics of nodes, so that in the cluster governance phase, through real-time monitoring, it can automatically intervene to migrate some Pods on the nodes with high resource utilization rate to the nodes with low utilization rate, when high resource utilization rate, node failure, and high number of Pods are observed.

The native descheduler only supports load-aware scheduling based on Pod request, which evicts Pods on nodes with higher utilization rates, thus equalizing resource utilization among nodes and avoiding overheating of individual node. However, Pod request does not reflect the real resource utilization of the nodes, so Volcano implements descheduling based on the real load of the nodes, by querying the metrics exposed by nodes, more accurate descheduling is performed based on the real load of CPU and Memory.

![LoadAware-EN](docs/img/descheduler_EN.svg)

The principle of LoadAware is shown in the figure above:

- Appropriately utilized nodes: nodes with resource utilization greater than or equal to 30% and less than or equal to 80%. The load level range of this node is a reasonable range expected to be reached.

- Over-utilized nodes: nodes with resource utilization higher than 80%. Hotspot nodes will evict some Pods and reduce the load level to no more than 80%. The descheduler will schedule the Pods on the hotspot nodes to the idle nodes.

- Under-utilized nodes: nodes with resource utilization lower than 30%.