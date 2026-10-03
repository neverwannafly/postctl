package diffview

func sizeTag(width int) string {
	switch width {
	case 80:
		return "80x24"
	case 120:
		return "120x40"
	default:
		return "200x60"
	}
}
