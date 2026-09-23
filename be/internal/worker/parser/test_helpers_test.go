package parser

func summaries(atoms []parseAtom) []string {
	out := make([]string, len(atoms))
	for i, atom := range atoms {
		if len(atom.Image) > 0 {
			out[i] = "img:" + atom.Ext
			continue
		}
		out[i] = atom.Text
	}
	return out
}
