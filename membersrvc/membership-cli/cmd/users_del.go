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

// deluserCmd represents the deluser command
var usersDelCmd = &cobra.Command{
	Use:   "del",
	Short: "del a user from ca server by id",
	Long: `del a user from ca server by id ex:
	
	membership-cli users del --id bob`,
	Run: func(cmd *cobra.Command, args []string) {
		// fmt.Println("deluser called")
		runUsersDelCmd();
	},
}

func init() {
	usersDelCmd.Flags().StringVarP(&Id, "id", "", "", "user id of user")
	usersCmd.AddCommand(usersDelCmd)
}

func runUsersDelCmd() {
	if err := rpcUsersDel(Url, Id); err != nil {
		fmt.Println("")
		fmt.Println(err)
		fmt.Println("")
	} else {
		fmt.Println("success")
	}
}