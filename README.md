# RHDH Model Catalog Bridge

This repository provides various containers that faciiltate theseamless export of AI model records from various AI Model Registres and imports them into Red Hat Developer Hub (Backstage) as catalog entities.

Current status: early POC stage.  
- We have some docker files in place for the container imaages, but more work is needed there
- This repository collaborates with Backstage catalog entensions currently hosted in [our fork of the RHDH plugins repository](https://github.com/redhat-ai-dev/rhdh-plugins/tree/main/workspaces/rhdh-ai).
- Until those plugins have assoicated images and can be added to OCP RHDH, we have to run those plugins, and by extension backstage, from our laptops.
- By extension, the `rhoai-normalizer` and `storage-reset` containers have to run on one's laptop as well.  The `location` container can run as an OCP deployment, but it is just as easy to run it out of your laptop as well.
- This [simple Gitops repo](https://github.com/redhat-ai-dev/odh-kubeflow-model-registry-setup) has the means of setting up Open Data Hub plus dev patches for Kubelow Model Registry that facilitate getting the URLs for running Models deployed into RHOAI/ODH by the Model Registry.


## Contributing

All contributions are welcome. The [Apache 2 license](http://www.apache.org/licenses/) is used and does not require any 
contributor agreement to submit patches. That said, the preference at this time for issue tracking is not GitHub issues
in this repository.  

Rather, visit the team's [RHDHPAI Jira project and the 'model-registry-bridge' component](https://issues.redhat.com/issues/?jql=project%20%3D%20RHDHPAI%20AND%20component%20%3D%20model-registry-bridge).

## Prerequisites

- An OpenShift cluster with 3x worker nodes, with at least 8 CPU, and 32GB memory each. 
   - on AWS `m6i.2xlarge` or `g5.2xlarge` (if GPUs are needed) work well
   - For other options, see https://aws.amazon.com/ec2/instance-types/

## Model Catalog Schema

The schema that the model catalog bridge will use to collect model and model server metadata can be found under [schema/](./schema/).

To re-generate the types that correspond to the schema, run `make generate-types-all`.

### RHOAI Model Registry mapping to our Model Catalog Schema

At this time, most of the model metadata expressed by the Model Catalog Schema does not have corresponding fields in the  
Kubeflow based Model Registry component's RegisteredModel and ModelVersion data types.  As a result, a AI platform engineer
who registers a model in RHOAI with the hopes of importing that model into RHDH must use the key/value pairs on the custom properties of the 
RegisteredModel and ModelVersion types.

To this end, the Model Catalog Bridge looks for these well know keys in the custom properties of the Registered Model and
Model Versions:

- `License` (waiting on schema update to capture license information); set by the RHOAI Model Catalog
- `Provider`: the value of this key will be a tag; set by the RHOAI Model Catalog
- `Registered From`: the value of this key will be a tag; set by the RHOAI Model Catalog
- `Source model`: the value of this key will be a tag; set by the RHOAI Model Catalog
- `Source model version`: the value of this key will be a tag; set by the RHOAI Model Catalog
- `Ethics`: mapped to the ethics field in the schema; set by the AI platform engineer
- `How to use`: mapped to the how to use field in the schema; set by the AI platform engineer
- `Support`: mapped to the support field in the schema; set by the AI platform engineer
- `Training`: mapped to the training field in the schema; set by the AI platform engineer
- `Usage`: mapped to the usage field in the shcema; set by the AI Platform engineer
- `Homepage URL`: mapped to homepage URL field in the schema; set by the AI Platform engineer
- `API Spec`: mapped to the API spec field in the schema; set by the AI Platform engineer
- `API Type`: mapped to the API type field in the schema; set by the AI Platform engineer
- `Owner`: mapped to the owner field in the schema; see section below for how this is collected 
- `Lifecycle`: mapped to the lifecycle field in the schema; see section below for hos this is collected

If the AI platform engineer feels any of these settings are common across multiple Model Versions (the Model Version is 
the level of the Model Registry API from where you deploy a model), they can use the custom properties of the Registered
Model.  Otherwise, using the custom properties of the Model Version is also accepted.

If any of these settings is Model Version specific, they the AI platform engineer should use the Model Version custom properties.

**NOTE** while the ModelArtifacts also have custom properties, the user cannot set it from the RHOAI console; at this time,
we will inspect any key/value pairs the Kubeflow controllers set, and if they meet Backstage naming criteria, they will be
added as additional tags.

### How the Owner and Lifecycle settings are collected

As the `owner` and `lifecycle` settings are required for all Backstage entities, a non empty value must be provided.

Also, in the case of `owner`, as it turns out, the Model Registry's Registered Model data type does hava an `owner` field.

So for `owner` the order of precedence is:

- take the `Owner` k/v pair's value from the Model Version's custom properites, otherwise from the Registered Model' custom properties
- use the Registered Model's `owner` field if set
- use the value of the `DEFAULT_OWNER` environment variable set on the rhoai-normalizer container
- otherwise, use the default of `rhdh-rhoai-bridge`

Then for `lifecycle` the order of precendence is:
- take the `Lifecycle` k/v pair's value from the Model Version's custom properites, otherwise from the Registered Model' custom properties
- use the value of the `DEFAULT_LIFECYCLE` environment variable set on the rhoai-normalizer container
- otherwise, use the default of `development`


### Model Catalog Schema to Backstage Entity settings and Backstage UI

**TO-DO**:  do we want to describe in this repository or the rhdh-plugins repository how the schema is mapped to
what developers see from the Backstage UI (vs. say whatever private Google Doc we might have for this).  
Seems conceivable that it would be a useful AI platform engineers, and a feeder into the documentation we ultimatley
create.

## Usage

Either via the command line, or from your favorite Golang editor, set the following environment variables as follows

### rhoai-normalizer

1. `K8S_TOKEN` - the login/bearer token of your `kubeadmin` user for the OCP cluster you are testing on
2. `KUBECONFIG` - the path to the local kubeconfig file corresponding to your OCP cluster
3. `MR_ROUTE` - the name of the Model Registry route in the `istio-system` namespace. For now, use `odh-model-registries-modelregistry-public-rest`.  We will make this a comma separated list of route entries.  Each entry can either be of the form `namespace:name` or just `name` (in which case the `istio-system` namespace will be assumed), to allow for multiple registries.  Also, the RHOAI model registry team is looking to remove Service Mesh as a dependency in RHOAI 1.22.  This may affect the default namespace (i.e. `istio-system`) where the RHOAI/Kubeflow model registry routes are stored, and how they are labeled for lookup.
4. `NAMESPACE` - the name of the namespace you create for deploying AI models from ODH
5. `STORAGE_URL` - for now, just use `http://localhost:7070`; this will be updated when we can run this container in OCP as part of the RHDH plugin running in RHDH
6. `NORMALIZER_FORMAT` - can either be `JsonArrayFormat` for our new format from the `schema` folder, or the legacy `CatalogInfoYamlFormat`; if not set defaults to `CatalogInfoYamlFormat` until RHDHPAI-611 and RHDHPAI-612 are completed.

### storage-rest

1. `RHDH_TOKEN` - the static token you create in backstage to allows for authenticated access to the Backstage catalog API.  See (https://github.com/redhat-ai-dev/rhdh-plugins/blob/main/workspaces/rhdh-ai/app-config.yaml#L19)[https://github.com/redhat-ai-dev/rhdh-plugins/blob/main/workspaces/rhdh-ai/app-config.yaml#L19]
2. `BKSTG_URL` - for now, just use `http://localhost:7007`; this will be updated when we can run this container in OCP as part of the RHDH plugin running in RHDH
3. `BRIDGE_URL` - for now, just use `http://localhost:9090`; this is the REST endpoint of our `location` container
4. `STORAGE_TYPE` - for now, only the development mode `ConfigMap` is supported; we'll add `GitHub` soon
5. `K8S_TOKEN`, `KUBECONFIG`, and `NAMESPACE` are the same as above
6. `NORMALIZER_FORMAT` - can either be `JsonArrayFormat` for our new format from the `schema` folder, or the legacy `CatalogInfoYamlFormat`; if not set defaults to `CatalogInfoYamlFormat` until RHDHPAI-611 and RHDHPAI-612 are completed.

### location

1. `STORAGE_URL` is the same as above
2. `NORMALIZER_FORMAT` - can either be `JsonArrayFormat` for our new format from the `schema` folder, or the legacy `CatalogInfoYamlFormat`; if not set defaults to `CatalogInfoYamlFormat` until RHDHPAI-611 and RHDHPAI-612 are completed.

When you are ready to launch the 3 processes, set your current namespace to the `NAMESPACE` value:

```
oc project $NAMESPACE
```

### Debugging (VS Code)

To debug the bridge services in VS Code, a launch.json file has been provided with options to debug each of the individual services. Ensure that you launch VS Code from the same terminal window that has the above environment variables set.

### Deploying a model

For our testing, we've been using the a cheap and relatively easy to deploy model the RHOAI team has posted on Hugging Face:  https://huggingface.co/tarilabs/mnist/resolve/v20231206163028/mnist.onnx

Use a "URL Connection" when registering in Model Registry and deploying from Model Registry.  ONNX is the model format.

Deploy the model in the `NAMESPACE` namespace noted above.  Also, in that namespace, creating the following `ServiceMeshMember` entry:

```
apiVersion: maistra.io/v1
kind: ServiceMeshMember
metadata:
  name: default
spec:
  controlPlaneRef:
    name: data-science-smcp
    namespace: istio-system
```

### Running as a RHDH Sidecar

**NOTE** this is intended for local development use only at this time.

The [sidecar](./assets/sidecar-after-ai-rhdh-installer) folder as a working set of YAML files that can be used the augment the configuration in the `ai-rhdh` namespace that is set up by the 
RHDHPAP team's [ai-rhdh-installer](https://github.com/redhat-ai-dev/ai-rhdh-installer).

The steps involved **AFTER** running the 'ai-rhdh-installer' are as follows:

- from the root of this repository, run `oc apply -f ./assets/sidecar-after-ai-rhdh-installer/k8s-sa-for-bridge.yaml` to set up the various ServiceAccounts, Secrets, RBAC, for the bridge to run as a set of 3 sidecar containers within the RHDH Pod.
- next, a list of hosts allowed to update the Backstage Catalog, and a static token for accessing the Backstage backend needs to be defined and set up; this is done in 2 parts.
- part one, the `ai-rh-developer-hub-env` Secret needs to be updated via say `oc edit` to include and `RHDH_TOKEN` key with a base 64 encoded randomly generated value; an YAML snippet for this exists in [this YAML file](assets/sidecar-after-ai-rhdh-installer/developer-hub-app-config-patches.yaml).
- this will force a restart of the RHDH pod; run `oc get pods -w` to see the old `backstage-ai-rh-developer-hub...` shut down and the new Pod spin up
- part two, the `app-config.extra.yaml` key in the `developer-hub-app-config` ConfigMap needs to be updated to include the new `RHDH_TOKEN` and the allowed hosts in the Backstage config
- if you look at the following bracketed section in [this YAML file](assets/sidecar-after-ai-rhdh-installer/developer-hub-app-config-patches.yaml) 

```yaml
    # start of delta from ai-rhdh-installer
    backend:
      auth:
        externalAccess:
          - type: static
            options:
              token: ${RHDH_TOKEN}
              subject: admin-curl-access
      reading:
        allow:
          - host: example.com
          - host: '*.mozilla.org'
          - host: '*.openshift.com'
          - host: '*.openshiftapps.com'
          - host: '10.*:9090'
          - host: '127.0.0.1:9090'
          - host: '127.0.0.1:8080'
          - host: '127.0.0.1:7070'
          - host: 'localhost:9090'
          - host: 'localhost:8080'
          - host: 'localhost:7070'
    # end of delta from ai-rhdh-installer
```

- while running `oc edit cm developer-hub-app-config` you will want to copy/paste that section after:

```yaml
    auth:
      environment: production
      providers:
        github:
          production:
            clientId: ${GITHUB__APP__CLIENT__ID}
            clientSecret: ${GITHUB__APP__CLIENT__SECRET}
```

- and before:

```yaml
    dangerouslyAllowSignInWithoutUserInCatalog: false
```

- the RHDH Pod will again recycle; watch for that to complete.
- lastly, we are now ready to update the `Backstage` CR instance with the new setting to include the 3 sidecar containers; again, a complicated patch, which we'll employ `oc edit backstage` from the `ai-rhdh` namespace.
- first, under `spec.application`, after the `extraEnvs:` line, paste from [./assets/sidecar-after-ai-rhdh-installer/backstage-cr.yaml](assets/sidecar-after-ai-rhdh-installer/backstage-cr.yaml) the following:

```yaml
    extraFiles:
      secrets:
        - name: rhdh-rhoai-bridge-token
          mountPath: /opt/app-root/src
          key: token
```

- then, replace the entire `deployment` section, which should look something like this:

```yaml
  deployment:
    patch:
      spec:
        replicas: 1
```

- with the entire `deployment` section from [./assets/sidecar-after-ai-rhdh-installer/backstage-cr.yaml](assets/sidecar-after-ai-rhdh-installer/backstage-cr.yaml)
- watch the `backstage-ai-rh-developer-hub...` Pod recycle (the new Pod will now have 4 containers, including the bridge's `location`, `storage-rest`, and `rhoai-normalizer` containers)
- after RHDH Pod restart has completed successfully, once models are defined in the ODH/RHOAI model registry, you'll see the `bac-import-model` ConfigMap populated with entries for those models 

### Deploying the Model Catalog RHDH Plugin

If you wish to run the Model Catalog plugin in a remote instance of RHDH, the steps are as follows:

- If your Backstage custom resource already has a dynamic plugins configmap listed under `spec.application.dynamicPluginsConfigMapName`, modify the ConfigMap to add the following entry under `dynamic-plugins.yaml`:

```
    plugins:
      - package: oci://quay.io/redhat-ai-dev/ai-integrations-rhdh:latest!red-hat-developer-hub-backstage-plugin-catalog-backend-module-model-catalog
        disabled: false
```

- If you do not have a ConfigMap listed under `spec.application.dynamicPluginsConfigMapName`, then you may use [./assets/sidecar-after-ai-rhdh-installer/dynamic-plugin-patches.yaml](./assets/sidecar-after-ai-rhdh-installer/dynamic-plugin-patches.yaml) as a reference point. 

    - After the ConfigMap has been created, modify the Backstage CR to specify the ConfigMap name under `spec.application.dynamicPluginsConfigMapName`
