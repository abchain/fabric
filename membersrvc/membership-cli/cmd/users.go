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
	// "fmt"

	"github.com/spf13/cobra"
)

// usersCmd represents the users command
var usersCmd = &cobra.Command{
	Use:   "users",
	Short: "fetch, add or del a user",
	Long: `fetch, add or del a user:

	'membership-cli users fetch'  return all users.
	'membership-cli users add --id zx --affiliation test --role 1 --password zxsss'  add user zx.
	'membership-cli users del --id zx'  del affiliaiton aff1.
	
	`,
	Run: func(cmd *cobra.Command, args []string) {
		// fmt.Println("users called")
		runUsersFetchCmd()
	},
}

func init() {
	usersCmd.Flags().StringVarP(&Affiliation, "affiliation", "", "", "affiliation of user")
	usersCmd.Flags().StringVarP(&Role, "role", "", "", "role of user to fetch")
	rootCmd.AddCommand(usersCmd)
}
