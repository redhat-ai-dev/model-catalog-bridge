# Model Catalog Schema

The goal of the [model catalog schema](./model-catalog.schema.json) is to provide a way to aggregate metadata for models and model servers into a consistent format that can be used to generate Backstage model catalog entities that represent them.

## Catalog Structure:
In this catalog: 
- Each model server is represented as a `Component` with type `model-server`, containing information such as:
   - Name, description URL, authentication status, and how to get access
- Each model deployed on a model server is represented as a `Resource` with type `ai-model`, containing information such as:
   - Name, description, model usage, intended tasks, tags, license, and author
- An `API` object representing the model server API type (of type `openai`, `grpc`, `graphql`, or `asyncapi`), which may include the API specification, and additional information about the model server's API.
- Each `model-server` Component `dependsOn`:
   - The 1 to N `ai-model` resources deployed on it
   - The `API` object associated with the model server

![AI Catalog](https://github.com/redhat-ai-dev/model-catalog-example/blob/main/assets/catalog-graph.png?raw=true "AI Catalog")

A reference model catalog schema can be found [here](https://github.com/redhat-ai-dev/model-catalog-example/blob/main/developer-model-service/catalog-info.yaml)

