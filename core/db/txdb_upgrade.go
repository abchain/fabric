package db

import (
	"fmt"
	"strings"
)

var globalDBUPgrade = map[int]func(*GlobalDataDB) int{
	1: upgradeToVersion2,
	2: upgradeToVersion3,
}

//ver 1 should not exist ...
func upgradeToVersion2(openchainDB *GlobalDataDB) int {
	return 2
}

//change the ley of db persisting
func upgradeToVersion3(openchainDB *GlobalDataDB) int {

	itr := openchainDB.GetIterator(PersistCF)
	defer itr.Close()

	for ; itr.Valid(); itr.Next() {

		if strings.Contains(string(itr.Key().Data()), currentDBKey) || strings.Contains(string(itr.Key().Data()), currentVersionKey) {

			k := string(itr.Key().Data())
			kparsered := strings.Split(k, ".")
			var newk string
			if lenK := len(kparsered); lenK == 1 {
				newk = strings.Join([]string{odbsettings, "", kparsered[0]}, ".")
			} else if lenK == 2 {
				newk = strings.Join([]string{odbsettings, kparsered[0], kparsered[1]}, ".")
			} else {
				panic(fmt.Errorf("Encounter unrecognized key [%s], it may require a manual repair", k))
			}

			dbLogger.Info("Find key [%s] contains db persisted data, upgrade it to %s", k, newk)
			if err := openchainDB.put(openchainDB.persistCF, []byte(newk), itr.Value().Data()); err != nil {
				panic(fmt.Errorf("Write DB fail: %s", err))
			}
			openchainDB.delete(openchainDB.persistCF, itr.Key().Data())
		}
	}

	return 3
}
