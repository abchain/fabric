// Copyright blackpai.com. 2018 All Rights Reserved.
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

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// attributesfetchCmd represents the fetchuserattributes command
var attributesfetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "get all attributes or get all attributes of one user",
	Long: `get all attributes or get all attributes of one user `,
	Run: func(cmd *cobra.Command, args []string) {
		// fmt.Println("fetchuserattributes called")
		runAttributesFetchCmd()
	},
}

func init() {
	attributesfetchCmd.Flags().StringVarP(&Id, "userid", "", "", "id of user")
	attributesfetchCmd.Flags().StringVarP(&Affiliation, "useraffiliation", "", "", "affiliation of user")
	attributesCmd.AddCommand(attributesfetchCmd)
}

func runAttributesFetchCmd() {
	if attrs, err := rpcAttributesFetch(Url, Id, Affiliation); err != nil {
		fmt.Println("")
		fmt.Println(err)
		fmt.Println("")
	} else {
		str := fmt.Sprintf("%15s %42s    %s to %s", "owner.id |", "name:value |", "validfrom to validto")
		fmt.Println("--------------------------------------------------------------------------------------------------------------------------------------")
		fmt.Println("    ", str)
		fmt.Println("--------------------------------------------------------------------------------------------------------------------------------------")
		for _, attr := range attrs {
			fmt.Println("- ", attr)
		}
		fmt.Println("")
	}
}