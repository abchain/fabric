# Dependency issues on referencing modules of YA-fabric 

YA-fabric fully vendoring its dependency which may bring some issues for developers who wish to use one or some of its modules. Following is some basic guidance for the most common case and:

### Following module is "exporting ready"

- All exports in consensus framework (concensus and its subdirectories)
- Node engine and running API (node and its subdirectories)
- Config module (core/config)
- Credential module (core/cred)
- Embedded chaincode API (core/embedded_chaincode)
- Event utilities (events/litekfk)

So the exported entries in all of modules above do not involve references to objects whose module has been vendored

### Caution to protobuf objects

Developer must not marshal/unmarshal any protobuf objects defined in YA-fabric in codes outside of YA-fabric, because
protobuf library has been vendored.

However, some protobuf objects has its own marsha/unmarshal API inside YA-fabric and user can call.

### Some common libraries, like viper and logging, all have their standalone implements inside YA-fabric

There are standalone version in YA-fabric being initialized when running node engine, and developer MUST do another initialization for these libraries in their own program.

The standalone version of logging can be access via "GetLogger" method in config module, and "SubViper" method provided the accessing of viper
