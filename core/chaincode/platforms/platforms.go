/*
Copyright IBM Corp. 2016 All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

		 http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package platforms

import (
	"archive/tar"
	"fmt"
	"github.com/abchain/fabric/core/chaincode/platforms/car"
	"github.com/abchain/fabric/core/chaincode/platforms/golang"
	"github.com/abchain/fabric/core/chaincode/platforms/java"
	"github.com/abchain/fabric/core/config"
	"github.com/abchain/fabric/core/util"
	pb "github.com/abchain/fabric/protos"
	"io"
	"os"
	"time"
)

// Interface for validating the specification and and writing the package for
// the given platform
type Platform interface {
	ValidateSpec(spec *pb.ChaincodeSpec) error

	/*
		YA-fabric 0.9：
		We have sepearted the platform-related chaincode deployment into two parts:
		1. Generate a bytecode for chaincode, which can be used for different runtime
		2. Generate data for a *specified* runtime, currently we have only docker, new
		method should be added if we have introduced more
	*/

	//for step 1: codepath is divided into two parts, the packpath part is reserved in
	//the chaincode bytecode
	//shouldclean indicate the code path should be remove after being used
	//the path argument MUST be the chaincodeID.Path in corresponding spec
	GetCodePath(string) (rootpath string, packpath string, shouldclean bool, err error)
	//for step 2: this suppose we have an archive of codes under the "ccfile" (magic string)
	//directory and platform should provided extra resources into the archive and a dockerfile
	//template which can correctly build the chaincode
	WriteDockerRunTime(spec *pb.ChaincodeSpec, tw *tar.Writer) (string, error)
}

// Find returns the platform interface for the given platform type
func Find(chaincodeType pb.ChaincodeSpec_Type) (Platform, error) {

	switch chaincodeType {
	case pb.ChaincodeSpec_GOLANG:
		return &golang.Platform{}, nil
	case pb.ChaincodeSpec_CAR:
		return &car.Platform{}, nil
	case pb.ChaincodeSpec_JAVA:
		return &java.Platform{}, nil
	default:
		return nil, fmt.Errorf("Unknown chaincodeType: %s", chaincodeType)
	}

}

func ValidateSpec(spec *pb.ChaincodeSpec) error {

	if spec == nil {
		return fmt.Errorf("Expected chaincode specification, nil received")
	}

	platform, err := Find(spec.Type)
	if err != nil {
		return fmt.Errorf("Failed to determine platform type: %s", err)
	}

	return platform.ValidateSpec(spec)

}

func WritePackage(spec *pb.ChaincodeSpec, out io.Writer) (string, error) {

	platform, err := Find(spec.Type)
	if err != nil {
		return "", err
	}

	return generateHashcode(spec, out, platform)
}

func WriteRunTime(spec *pb.ChaincodeSpec, clispec *config.ClientSpec, tw *tar.Writer) error {

	platform, err := Find(spec.Type)
	if err != nil {
		return err
	}

	zeroTime := time.Time{}
	//write docker file and other specs ...
	dockerfile, err := platform.WriteDockerRunTime(spec, tw)
	if err != nil {
		return fmt.Errorf("platform fail to write runtime into package", err)
	}

	if clispec.EnableTLS {
		//write tls certificate and append dockerfile
		ca, err := os.Open(util.CanonicalizeFilePath(clispec.TLSRootCertFile))
		if err != nil {
			return fmt.Errorf("fail to open certificate file %s: %s", clispec.TLSRootCertFile, err)
		}
		fstat, err := ca.Stat()
		if err != nil {
			return fmt.Errorf("fail to state certificate file %s: %s", clispec.TLSRootCertFile, err)
		}
		tw.WriteHeader(&tar.Header{Name: "cert/ca.crt", Size: fstat.Size(), ModTime: zeroTime, AccessTime: zeroTime, ChangeTime: zeroTime})
		_, err = io.Copy(tw, ca)
		if err != nil {
			return fmt.Errorf("fail to open certificate file %s: %s", clispec.TLSRootCertFile, err)
		}
		dockerfile = fmt.Sprintf("%s\nCOPY cert/ca.crt .\n", dockerfile)
	}

	dockerfileSize := int64(len([]byte(dockerfile)))

	tw.WriteHeader(&tar.Header{Name: "Dockerfile", Size: dockerfileSize, ModTime: zeroTime, AccessTime: zeroTime, ChangeTime: zeroTime})
	tw.Write([]byte(dockerfile))

	return nil
}

func GetArgsAndEnv(spec *pb.ChaincodeSpec, clispec *config.ClientSpec) (args []string, envs []string, err error) {
	cID := spec.ChaincodeID
	cLang := spec.Type

	envs = []string{"CORE_CHAINCODE_ID_NAME=" + cID.Name}
	if clispec.EnableTLS {
		envs = append(envs, "CORE_PEER_TLS_ENABLED=true")
		envs = append(envs, "CORE_PEER_TLS_ROOTCERT_FILE=ca.crt")
		if clispec.TLSHostOverride != "" {
			envs = append(envs, "CORE_PEER_TLS_SERVERHOSTOVERRIDE="+clispec.TLSHostOverride)
		}
	}

	switch cLang {
	case pb.ChaincodeSpec_GOLANG, pb.ChaincodeSpec_CAR:
		//chaincode executable will be same as the name of the chaincode, and install at
		//the bin path of working path (GOPATH)
		args = []string{"bin/" + cID.Name, fmt.Sprintf("-peer.address=%s", clispec.Address)}
	case pb.ChaincodeSpec_JAVA:
		//TODO add security args
		args = []string{"java",
			"-jar chaincode.jar",
			fmt.Sprintf("-a %s", clispec.Address),
			fmt.Sprintf("-i %s", cID.Name),
		}
		if clispec.EnableTLS {
			args = append(args, "-s")
		}
	default:
		return nil, nil, fmt.Errorf("Unknown chaincodeType: %s", cLang)
	}
	return args, envs, nil

}
