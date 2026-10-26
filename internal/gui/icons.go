package gui

import (
	"gioui.org/widget"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

func mustIcon(data []byte) *widget.Icon {
	ic, err := widget.NewIcon(data)
	if err != nil {
		panic(err)
	}
	return ic
}

// Material Design icons used by the consoles.
var (
	IconDashboard  = mustIcon(icons.ActionDashboard)
	IconPeople     = mustIcon(icons.SocialPeople)
	IconSwap       = mustIcon(icons.ActionSwapHoriz)
	IconLogs       = mustIcon(icons.ActionSubject)
	IconSettings   = mustIcon(icons.ActionSettings)
	IconInfo       = mustIcon(icons.ActionInfoOutline)
	IconPlay       = mustIcon(icons.AVPlayArrow)
	IconStop       = mustIcon(icons.AVStop)
	IconRefresh    = mustIcon(icons.NavigationRefresh)
	IconCopy       = mustIcon(icons.ContentContentCopy)
	IconDelete     = mustIcon(icons.ActionDelete)
	IconAdd        = mustIcon(icons.ContentAdd)
	IconVisible    = mustIcon(icons.ActionVisibility)
	IconHidden     = mustIcon(icons.ActionVisibilityOff)
	IconGenerate   = mustIcon(icons.ActionAutorenew)
	IconWarning    = mustIcon(icons.AlertWarning)
	IconError      = mustIcon(icons.AlertErrorOutline)
	IconCheck      = mustIcon(icons.ActionCheckCircle)
	IconMoon       = mustIcon(icons.ImageBrightness2)
	IconSun        = mustIcon(icons.ImageWBSunny)
	IconLock       = mustIcon(icons.ActionLockOutline)
	IconVPN        = mustIcon(icons.CommunicationVPNKey)
	IconPower      = mustIcon(icons.ActionPowerSettingsNew)
	IconFolder     = mustIcon(icons.FileFolderOpen)
	IconSearch     = mustIcon(icons.ActionSearch)
	IconClose      = mustIcon(icons.NavigationClose)
	IconDone       = mustIcon(icons.ActionDone)
	IconDown       = mustIcon(icons.NavigationArrowDownward)
	IconSave       = mustIcon(icons.ContentSave)
	IconUndo       = mustIcon(icons.ContentUndo)
	IconRouter     = mustIcon(icons.HardwareRouter)
	IconDNS        = mustIcon(icons.ActionDNS)
	IconLanguage   = mustIcon(icons.ActionLanguage)
	IconHistory    = mustIcon(icons.ActionHistory)
	IconTimeline   = mustIcon(icons.ActionTimeline)
	IconComputer   = mustIcon(icons.HardwareComputer)
	IconFilter     = mustIcon(icons.ContentFilterList)
	IconVerified   = mustIcon(icons.ActionVerifiedUser)
	IconUpload     = mustIcon(icons.FileFileUpload)
	IconTune       = mustIcon(icons.ImageTune)
	IconChevron    = mustIcon(icons.NavigationChevronRight)
	IconOpen       = mustIcon(icons.ActionOpenInNew)
	IconDot        = mustIcon(icons.AVFiberManualRecord)
	IconLockClosed = mustIcon(icons.ActionLock)
)
