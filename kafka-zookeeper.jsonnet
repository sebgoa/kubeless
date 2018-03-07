local k = import "ksonnet.beta.1/k.libsonnet";

local statefulset = k.apps.v1beta1.statefulSet;
local container = k.core.v1.container;
local service = k.core.v1.service;
local deployment = k.apps.v1beta1.deployment;
local serviceAccount = k.core.v1.serviceAccount;

local namespace = "kubeless";
local controller_account_name = "controller-acct";

local controllerContainer =
  container.default("kafka-trigger-controller", "bitnami/kafka-trigger-controller:latest") +
  container.imagePullPolicy("IfNotPresent");

local kubelessLabel = {kubeless: "kafka-trigger-controller"};

local controllerAccount =
  serviceAccount.default(controller_account_name, namespace);

local controllerDeployment =
  deployment.default("kafka-trigger-controller", controllerContainer, namespace) +
  {metadata+:{labels: kubelessLabel}} +
  {spec+: {selector: {matchLabels: kubelessLabel}}} +
  {spec+: {template+: {spec+: {serviceAccountName: controllerAccount.metadata.name}}}} +
  {spec+: {template+: {metadata: {labels: kubelessLabel}}}};

local kafkaEnv = [
  {
    name: "KAFKA_ADVERTISED_HOST_NAME",
    value: "broker.kubeless"
  },
  {
    name: "KAFKA_ADVERTISED_PORT",
    value: "9092"
  },
  {
    name: "KAFKA_PORT",
    value: "9092"
  },
  {
    name: "KAFKA_DELETE_TOPIC_ENABLE",
    value: "true"
  },
  {
    name: "KAFKA_ZOOKEEPER_CONNECT",
    value: "zookeeper.kubeless:2181"
  },
  {
    name: "ALLOW_PLAINTEXT_LISTENER",
    value: "yes"
  }
];

local zookeeperEnv = [
  {
    name: "ZOO_SERVERS",
    value: "server.1=zoo-0.zoo:2888:3888:participant"
  },
  {
    name: "ALLOW_ANONYMOUS_LOGIN",
    value: "yes"
  }
];

local zookeeperPorts = [
  {
    containerPort: 2181,
    name: "client"
  },
  {
    containerPort: 2888,
    name: "peer"
  },
  {
    containerPort: 3888,
    name: "leader-election"
  }
];

local kafkaContainer =
  container.default("broker", "bitnami/kafka@sha256:0c4be25cd3b31176a4c738da64d988d614b939021bedf7e1b0cc72b37a071ecb") +
  container.imagePullPolicy("IfNotPresent") +
  container.env(kafkaEnv) +
  container.ports({containerPort: 9092}) +
  container.livenessProbe({tcpSocket: {port: 9092}, initialDelaySeconds: 30}) +
  container.volumeMounts([
    {
      name: "datadir",
      mountPath: "/bitnami/kafka/data"
    }
  ]);

local kafkaInitContainer =
  container.default("volume-permissions", "busybox") +
  container.imagePullPolicy("IfNotPresent") +
  container.command(["sh", "-c", "chmod -R g+rwX /bitnami"]) +
  container.volumeMounts([
    {
      name: "datadir",
      mountPath: "/bitnami/kafka/data"
    }
  ]);

local zookeeperContainer =
  container.default("zookeeper", "bitnami/zookeeper@sha256:f66625a8a25070bee18fddf42319ec58f0c49c376b19a5eb252e6a4814f07123") +
  container.imagePullPolicy("IfNotPresent") +
  container.env(zookeeperEnv) +
  container.ports(zookeeperPorts) +
  container.volumeMounts([
    {
      name: "zookeeper",
      mountPath: "/bitnami/zookeeper"
    }
  ]);

local zookeeperInitContainer =
  container.default("volume-permissions", "busybox") +
  container.imagePullPolicy("IfNotPresent") +
  container.command(["sh", "-c", "chmod -R g+rwX /bitnami"]) +
  container.volumeMounts([
    {
      name: "zookeeper",
      mountPath: "/bitnami/zookeeper"
    }
  ]);

local kafkaLabel = {kubeless: "kafka"};
local zookeeperLabel = {kubeless: "zookeeper"};

local kafkaVolumeCT = [
  {
    "metadata": {
      "name": "datadir"
    },
    "spec": {
      "accessModes": [
        "ReadWriteOnce"
      ],
      "resources": {
        "requests": {
          "storage": "1Gi"
        }
      }
    }
  }
];

local zooVolumeCT = [
  {
    "metadata": {
      "name": "zookeeper"
    },
    "spec": {
      "accessModes": [
        "ReadWriteOnce"
      ],
      "resources": {
        "requests": {
          "storage": "1Gi"
        }
      }
    }
  }
];

local kafkaSts =
  statefulset.default("kafka", namespace) +
  statefulset.spec({serviceName: "broker"}) +
  {spec+: {template: {metadata: {labels: kafkaLabel}}}} +
  {spec+: {volumeClaimTemplates: kafkaVolumeCT}} +
  {spec+: {template+: {spec: {containers: [kafkaContainer], initContainers: [kafkaInitContainer]}}}};

local zookeeperSts =
  statefulset.default("zoo", namespace) +
  statefulset.spec({serviceName: "zoo"}) +
  {spec+: {template: {metadata: {labels: zookeeperLabel}}}} +
  {spec+: {volumeClaimTemplates: zooVolumeCT}} +
  {spec+: {template+: {spec: {containers: [zookeeperContainer], initContainers: [zookeeperInitContainer]}}}};

local kafkaSvc =
  service.default("kafka", namespace) +
  service.spec(k.core.v1.serviceSpec.default()) +
  service.mixin.spec.ports({port: 9092}) +
  service.mixin.spec.selector({kubeless: "kafka"});

local kafkaHeadlessSvc =
  service.default("broker", namespace) +
  service.spec(k.core.v1.serviceSpec.default()) +
  service.mixin.spec.ports({port: 9092}) +
  service.mixin.spec.selector({kubeless: "kafka"}) +
  {spec+: {clusterIP: "None"}};

local zookeeperSvc =
  service.default("zookeeper", namespace) +
  service.spec(k.core.v1.serviceSpec.default()) +
  service.mixin.spec.ports({port: 2181, name: "client"}) +
  service.mixin.spec.selector({kubeless: "zookeeper"});

local zookeeperHeadlessSvc =
  service.default("zoo", namespace) +
  service.spec(k.core.v1.serviceSpec.default()) +
  service.mixin.spec.ports([{port: 9092, name: "peer"},{port: 3888, name: "leader-election"}]) +
  service.mixin.spec.selector({kubeless: "zookeeper"}) +
  {spec+: {clusterIP: "None"}};

{
  kafkaSts: k.util.prune(kafkaSts),
  zookeeperSts: k.util.prune(zookeeperSts),
  kafkaSvc: k.util.prune(kafkaSvc),
  kafkaHeadlessSvc: k.util.prune(kafkaHeadlessSvc),
  zookeeperSvc: k.util.prune(zookeeperSvc),
  zookeeperHeadlessSvc: k.util.prune(zookeeperHeadlessSvc),
  controller: k.util.prune(controllerDeployment),
}
