package controllers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/62teknologi/62seahorse/62golib/utils"
	"github.com/62teknologi/62seahorse/app/app_constant"
	"github.com/62teknologi/62seahorse/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ModerationController struct {
	SingularName        string
	PluralName          string
	SingularLabel       string
	PluralLabel         string
	Table               string
	ModuleName		   string
	ModerationTableSingularName  string
	ModerationTablePluralName    string
	ModerationTableSingularLabel string
	ModerationTableTable         string
	SequenceSuffixSingularName  string
	SequenceSuffixPluralName    string
	SequenceSuffixSingularLabel string
	SequenceSuffixTable         string
}

func (ctrl *ModerationController) Init(ctx *gin.Context) {
	ctrl.SingularName = utils.Pluralize.Singular(ctx.Param("table"))
	ctrl.PluralName = utils.Pluralize.Plural(ctx.Param("table"))
	ctrl.SingularLabel = ctrl.SingularName
	ctrl.PluralLabel = ctrl.PluralName
	ctrl.Table = ctrl.PluralName
	ctrl.ModuleName = config.Data.ModuleName
	ctrl.ModerationTableSingularName = utils.Pluralize.Singular(config.Data.ModerationTable)
	ctrl.ModerationTablePluralName = utils.Pluralize.Plural(config.Data.ModerationTable)
	ctrl.ModerationTableSingularLabel = ctrl.ModerationTableSingularName
	ctrl.ModerationTableTable = ctrl.ModerationTablePluralName
	ctrl.SequenceSuffixSingularName = utils.Pluralize.Singular(config.Data.SequenceSuffix)
	ctrl.SequenceSuffixPluralName = utils.Pluralize.Plural(config.Data.SequenceSuffix)
	ctrl.SequenceSuffixSingularLabel = ctrl.SequenceSuffixSingularName
	ctrl.SequenceSuffixTable = ctrl.SequenceSuffixPluralName
}

func (ctrl ModerationController) Create(ctx *gin.Context) {
	ctrl.Init(ctx)

	transformer, err := utils.JsonFileParser(config.Data.SettingPath + "/transformers/request/create.json")

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.ResponseData("error", err.Error(), nil))
		return
	}

	input := utils.ParseForm(ctx)

	if validation, err := utils.Validate(input, transformer); err {
		ctx.JSON(http.StatusBadRequest, utils.ResponseData("error", "validation", validation.Errors))
		return
	}

	utils.MapValuesShifter(transformer, input)
	utils.MapNullValuesRemover(transformer)

	if err = utils.DB.Transaction(func(tx *gorm.DB) error {
		recordRef := make(map[string]any)
		if err = tx.Table(ctrl.Table).Where("id = ?", transformer["ref_id"]).Take(&recordRef).Error; err != nil {
			return err
		}

		pivotTable := make(map[string]any)
		if err = tx.Table(ctrl.SingularName+"_"+ctrl.ModerationTablePluralName).Where("record_id = ?", transformer["ref_id"]).Order("id desc").Take(&pivotTable).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		createModeration := make(map[string]any)
		createModeration["requested_by"] = transformer["user_id"]
		createModeration["step_total"] = len(transformer["sequence"].([]any))
		createModeration["is_in_order"] = transformer["is_in_order"]
		createModeration["uuid"] = uuid.New().String()
		createModeration["status"] = 100

		if pivotTable["moderation_id"] != nil {
			moderationCheck := make(map[string]any)
			if err = tx.Table(ctrl.ModuleName + "_" + ctrl.ModerationTableTable).Where("id = ?", pivotTable["moderation_id"]).Take(&moderationCheck).Error; err != nil {
				return err
			}

			if moderationCheck["status"] != nil {
				moderationStatus := fmt.Sprintf("%v", moderationCheck["status"])

				if moderationStatus == fmt.Sprintf("%v", app_constant.Pending) {
					return errors.New("Moderation is already exist")
				}

				if moderationStatus == fmt.Sprintf("%v", app_constant.Approve) {
					return errors.New("Moderation is already approved")
				}

				if moderationStatus == fmt.Sprintf("%v", app_constant.Reject) {
					return errors.New("Moderation is already rejected")
				}
			}

			createModeration["parent_id"] = pivotTable["moderation_id"]

		}

		if err = tx.Table("mod_" + ctrl.ModerationTableTable).Create(&createModeration).Error; err != nil {
			return err
		}

		moderation := map[string]any{}
		tx.Table("mod_" + ctrl.ModerationTableTable).Where(createModeration).Take(&moderation)

		if transformer["sequence"] != nil {
			for i, v := range transformer["sequence"].([]any) {
				createModerationSequence := make(map[string]any)
				createModerationSequence["moderation_id"] = moderation["id"]
				createModerationSequence["result"] = 100
				createModerationSequence["uuid"] = uuid.New().String()

				if fmt.Sprintf("%v", moderation["is_in_order"]) == fmt.Sprintf("%v", 1) {
					createModerationSequence["step"] = i + 1
					if i == 0 {
						createModerationSequence["is_current"] = true
					}
				}

				if err = tx.Table("mod_" + ctrl.ModerationTableSingularName + "_" + ctrl.SequenceSuffixTable).Create(&createModerationSequence).Error; err != nil {
					return err
				}

				userIds := v.(map[string]any)["user_ids"]
				if userIds != nil {
					moderationSequence := make(map[string]any)
					tx.Table("mod_" + ctrl.ModerationTableSingularName + "_" + ctrl.SequenceSuffixTable).Where(createModerationSequence).Take(&moderationSequence)
					createModerationSequenceUsers := []map[string]any{}
					for _, w := range userIds.([]any) {
						cmu := map[string]any{
							"moderation_sequence_id": moderationSequence["id"],
							"user_id":                w,
						}

						createModerationSequenceUsers = append(createModerationSequenceUsers, cmu)
					}

					if err = tx.Table("mod_" + ctrl.ModerationTableSingularName + "_users").Create(&createModerationSequenceUsers).Error; err != nil {
						return err
					}
				}
			}
		}

		createPivot := make(map[string]any)
		createPivot["moderation_id"] = moderation["id"]
		createPivot["record_id"] = transformer["ref_id"]

		if err = tx.Table(ctrl.SingularName + "_" + ctrl.ModerationTableTable).Create(&createPivot).Error; err != nil {
			return err
		}

		return nil
	}); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.ResponseData("error", err.Error(), nil))
		return
	}

	ctx.JSON(http.StatusOK, utils.ResponseData("success", "create "+ctrl.ModerationTableSingularLabel+" "+ctrl.ModerationTableTable+" success", transformer))
}
