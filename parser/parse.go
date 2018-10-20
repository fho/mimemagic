package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	defaultPriority = 50
	invalidPriority = -1
	file            = 1
	directory       = 2
	link            = 3
)

func globMatcher(g *parsedGlob, mimeType int) (identifier, error) {
	s := g.Pattern
	ast := strings.IndexByte(s, '*')
	sqO := strings.IndexByte(s, '[')
	sqC := strings.IndexByte(s, ']')
	if sqO < 0 && sqC < 0 {
		switch ast {
		case -1:
			return simpleText{text(s), g.CaseSensitive, mimeType, g.Weight}, nil
		case 0:
			return simpleSuffix{suffix(s[1:]), g.CaseSensitive, mimeType, g.Weight}, nil
		case len(s) - 1:
			return simplePrefix{prefix(s[:ast]), g.CaseSensitive, mimeType, g.Weight}, nil
		default:
			return nil, errors.New("invalid glob pattern")
		}
	}
	if sqC < sqO || sqC < 0 || sqO < 0 {
		return nil, errors.New("invalid glob pattern")
	}
	type part struct {
		s string
		b bool
	}
	parts := make([]part, 0)
	if sqO != 0 {
		parts = append(parts, part{s[:sqO], false})
	}
	s = s[sqO+1:]
	sqC -= sqO + 1
	for {
		sqO = strings.IndexByte(s, '[')
		if sqC > sqO && sqO >= 0 {
			return nil, errors.New("invalid glob pattern")
		}
		parts = append(parts, part{s[:sqC], true})
		s = s[sqC+1:]
		if s == "" {
			break
		}
		if sqO < 0 {
			parts = append(parts, part{s, false})
			break
		}
		sqO -= sqC + 1
		sqC = strings.IndexByte(s, ']')
		if sqC < sqO || sqC < 0 {
			return nil, errors.New("invalid glob pattern")
		}
		if sqO != 0 {
			parts = append(parts, part{s[:sqO], false})
		}
		s = s[sqO+1:]
		sqC -= sqO + 1
	}
	prefix, suffix := false, false
	switch ast {
	case 0:
		suffix = true
		parts[0].s = parts[0].s[1:]
		if len(parts[0].s) == 0 {
			parts = parts[1:]
		}
		//for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		//	parts[i], parts[j] = parts[j], parts[i]
		//}
	case len(g.Pattern) - 1:
		prefix = true
		l := len(parts) - 1
		ll := len(parts[l].s) - 1
		parts[l].s = parts[l].s[:ll]
		if len(parts[l].s) == 0 {
			parts = parts[:l]
		}
	case -1:
		break
	default:
		return nil, errors.New("invalid glob pattern")
	}
	m := pattern{make([]matcher, 0, len(parts)), g.CaseSensitive,
		prefix, suffix, mimeType, g.Weight}
	for i := range parts {
		if !parts[i].b {
			m.Pattern = append(m.Pattern, text(parts[i].s))
		} else {
			s := parts[i].s
			dash := strings.IndexByte(s, '-')
			if dash < 0 {
				m.Pattern = append(m.Pattern, list(s))
				continue
			}
			if dash == 1 && len(s) == 3 && s[2] > s[0] {
				m.Pattern = append(m.Pattern, byteRange{s[0], s[2]})
				continue
			}
			var any anyOf
			for len(s) > 0 {
				switch {
				case dash == 0, dash == len(s)-1, dash == 1 && s[0] > s[2]:
					return nil, errors.New("invalid glob pattern")
				case dash == 1:
					any = append(any, byteRange{s[0], s[2]})
					s = s[3:]
					dash = strings.IndexByte(s, '-')
				case dash < 0:
					dash = len(s) + 1
					fallthrough
				default:
					any = append(any, list(s[:dash-1]))
					s = s[dash-1:]
					dash = 1
				}
			}
			m.Pattern = append(m.Pattern, any)
		}
	}
	return m, nil
}

func (p *parsedMIMEType) merge(n *parsedMIMEType) {
	//if n.Comment != "" {
	//	//if !strings.EqualFold(p.Comment, n.Comment) {
	//	//	fmt.Println(p.Comment+",", n.Comment)
	//	//}
	//	p.Comment = n.Comment
	//}
	//if n.Acronym != "" {
	//	//if !strings.EqualFold(p.Acronym, n.Acronym) {
	//	//	fmt.Println(p.Acronym+",", n.Acronym)
	//	//}
	//	p.Acronym = n.Acronym
	//}
	//if n.ExpandedAcronym != "" {
	//	//if !strings.EqualFold(p.ExpandedAcronym, n.ExpandedAcronym) {
	//	//	fmt.Println(p.ExpandedAcronym+",", n.ExpandedAcronym)
	//	//}
	//	p.ExpandedAcronym = n.ExpandedAcronym
	//}
	//if n.Icon != "" {
	//	p.Icon = n.Icon
	//}
	//if n.GenericIcon != "" {
	//	if !strings.EqualFold(p.GenericIcon, n.GenericIcon) {
	//		fmt.Println(p.Subtype, p.GenericIcon+",", n.GenericIcon)
	//	}
	//	p.GenericIcon = n.GenericIcon
	//}
	if p.Comment == "" {
		p.Comment = n.Comment
	}
	if p.Acronym == "" {
		p.Acronym = n.Acronym
	}
	if p.ExpandedAcronym == "" {
		p.ExpandedAcronym = n.ExpandedAcronym
	}
	if p.Icon == "" {
		p.Icon = n.Icon
	}
	if p.GenericIcon == "" {
		p.GenericIcon = n.GenericIcon
	}
	if len(n.Alias) > 0 {
		slc := append(p.Alias, n.Alias...)
		strmap := make(map[string]struct{}, len(slc))
		p.Alias = make([]string, 0, len(slc))
		for _, s := range slc {
			if _, ok := strmap[s]; ok || p.Media+"/"+p.Subtype == s {
				continue
			}
			strmap[s] = struct{}{}
			p.Alias = append(p.Alias, s)

		}
	}
	if len(n.SubClassOf) > 0 {
		slc := append(p.SubClassOf, n.SubClassOf...)
		strmap := make(map[string]struct{}, len(slc))
		p.SubClassOf = make([]string, 0, len(slc))
		for _, s := range slc {
			if _, ok := strmap[s]; ok || p.Media+"/"+p.Subtype == s {
				continue
			}
			strmap[s] = struct{}{}
			p.SubClassOf = append(p.SubClassOf, s)

		}
	}
	if len(n.Extension) > 0 {
		slc := append(p.Extension, n.Extension...)
		strmap := make(map[string]struct{}, len(slc))
		p.Extension = make([]string, 0, len(slc))
		for _, s := range slc {
			if _, ok := strmap[s]; ok {
				continue
			}
			strmap[s] = struct{}{}
			p.Extension = append(p.Extension, s)

		}
	}
	if len(n.Glob) > 0 {
		slc := append(p.Glob, n.Glob...)
		p.Glob = mergeGlobs(slc...)
	}
	if len(n.RootXML) > 0 {
		slc := append(p.RootXML, n.RootXML...)
		xmlmap := make(map[string]*parsedRootXML, len(slc))
		p.RootXML = make([]*parsedRootXML, 0, len(slc))
		for _, r := range slc {
			if rr, ok := xmlmap[r.NamespaceURI]; ok && rr.LocalName == r.LocalName {
				continue
			}
			xmlmap[r.NamespaceURI] = r
			p.RootXML = append(p.RootXML, r)
		}
	}
	if len(n.Magic) > 0 {
		p.Magic = append(p.Magic, n.Magic...)
	}
	if len(n.TreeMagic) > 0 {
		p.TreeMagic = append(p.TreeMagic, n.TreeMagic...)
	}
}

func parseMIMEInfo(m *mimeInfo) (parsedMIMEInfo, error) {
	if len(m.MIMEType) < 1 {
		return nil, errors.New("<mime-info> must contain at least one <mime-type> element")
	}
	p := make(parsedMIMEInfo, len(m.MIMEType))
	for i, mimeType := range m.MIMEType {
		mt, err := parseMIMEType(mimeType)
		if err != nil {
			return nil, fmt.Errorf("error parsing %s: %v", mimeType.Type, err)
		}
		p[i] = mt

	}
	return p, nil
}

func parseMIMEType(m *mimeType) (*parsedMIMEType, error) {
	s := strings.Split(m.Type, "/")
	if len(s) != 2 {
		return nil, fmt.Errorf("unknown media type in type '%s'", m.Type)
	}
	switch s[0] {
	case "all", "uri", "print", "text", "application", "image", "audio", "inode", "video",
		"message", "model", "multipart", "x-content", "x-epoc", "x-scheme-handler", "font":
		break
	default:
		return nil, fmt.Errorf("Unknown media type in type '%s'", m.Type)
	}
	p := &parsedMIMEType{
		Media:   s[0],
		Subtype: s[1],
	}
outer:
	for _, comment := range m.Comment {
		switch comment.Lang {
		case "", "en", "en_GB":
			if comment.Value != "" {
				p.Comment = comment.Value
				break outer
			}
		}
	}
	//if p.Comment == "" {
	//	return nil, errors.New("<comment> is missing or empty")
	//}
	if m.Acronym != nil {
		p.Acronym = m.Acronym.Value
	}
	if m.ExpandedAcronym != nil {
		p.ExpandedAcronym = m.ExpandedAcronym.Value
	}
	if m.Icon != nil {
		p.Icon = m.Icon.Name
	}
	if m.GenericIcon != nil {
		p.GenericIcon = m.GenericIcon.Name
	}
	for _, alias := range m.Alias {
		p.Alias = append(p.Alias, alias.Type)
	}
	for _, subclass := range m.SubClassOf {
		p.SubClassOf = append(p.SubClassOf, subclass.Type)
	}
	for _, glob := range m.Glob {
		s := glob.Pattern
		if len(s) > 2 && s[0] == '*' && s[1] == '.' && strings.IndexByte(s, '[') < 0 {
			p.Extension = append(p.Extension, s[1:])
		}
		pp, err := parseGlob(glob)
		if err != nil {
			return nil, err
		}
		p.Glob = append(p.Glob, pp)
	}
	p.Glob = mergeGlobs(p.Glob...)
	for _, rootXML := range m.RootXML {
		rx, err := parseRootXML(rootXML)
		if err != nil {
			return nil, err
		}
		p.RootXML = append(p.RootXML, rx)
	}
	for _, magic := range m.Magic {
		ma, err := parseMagic(magic)
		if err != nil {
			return nil, err
		}
		p.Magic = append(p.Magic, ma)
	}
	for _, treeMagic := range m.TreeMagic {
		tm, err := parseTreeMagic(treeMagic)
		if err != nil {
			return nil, err
		}
		p.TreeMagic = append(p.TreeMagic, tm)
	}
	return p, nil
}

func parseRootXML(r *rootXML) (*parsedRootXML, error) {
	if r.NamespaceURI+r.LocalName == "" {
		return nil, errors.New("namespaceURI and localName attributes can't both be empty")
	}
	if strings.ContainsAny(r.NamespaceURI+r.LocalName, " \n") {
		return nil, errors.New("namespaceURI and localName cannot contain spaces or newlines")
	}
	return &parsedRootXML{
		NamespaceURI: r.NamespaceURI,
		LocalName:    r.LocalName,
	}, nil
}

func parseTreeMagic(t *treeMagic) (p *parsedTreeMagic, err error) {
	p = &parsedTreeMagic{Priority: getPriority(t.Priority)}
	if p.Priority == invalidPriority {
		return nil, errors.New("invalid tree magic priority")
	}
	for _, mm := range t.TreeMatch {
		pp, err := parseTreeMatch(mm)
		if err != nil {
			return nil, err
		}
		p.TreeMatch = append(p.TreeMatch, pp)
	}
	return
}

func parseTreeMatch(t *treeMatch) (*parsedTreeMatch, error) {
	if t.Path == "" {
		return nil, errors.New("missing 'path' attribute in <treematch>")
	}
	p := &parsedTreeMatch{
		Path:       t.Path,
		MatchCase:  t.MatchCase,
		Executable: t.Executable,
		NonEmpty:   t.NonEmpty,
	}
	switch t.Type {
	case "":
		break
	case "file":
		p.Type = file
	case "directory":
		p.Type = directory
	case "link":
		p.Type = link
	default:
		return nil, errors.New("invalid 'type' attribute in <treematch>")
	}
	for _, mm := range t.TreeMatch {
		pp, err := parseTreeMatch(mm)
		if err != nil {
			return nil, err
		}
		p.TreeMatch = append(p.TreeMatch, pp)
	}
	return p, nil
}

func getPriority(p *int) int {
	if p == nil {
		return defaultPriority
	}
	i := *p
	if i < 0 || i > 100 {
		return invalidPriority
	}
	return i
}

func parseGlob(g *glob) (p *parsedGlob, err error) {
	p = &parsedGlob{
		Weight:        getPriority(g.Weight),
		CaseSensitive: g.CaseSensitive,
	}
	if p.Weight == invalidPriority {
		return nil, errors.New("invalid glob weight")
	}
	if g.Pattern == "" {
		return nil, errors.New("missing glob pattern")
	}
	if !p.CaseSensitive {
		p.Pattern = strings.ToLower(g.Pattern)
	} else {
		p.Pattern = g.Pattern
	}
	return
}

func mergeGlobs(pp ...*parsedGlob) []*parsedGlob {
	type gpattern struct {
		pattern string
		cs      bool
	}
	p := make([]*parsedGlob, 0, 1)
	globmap := make(map[gpattern]*parsedGlob)
	for _, ppp := range pp {
		gp := gpattern{ppp.Pattern, ppp.CaseSensitive}
		if v, ok := globmap[gp]; ok && v.CaseSensitive == ppp.CaseSensitive && ppp.Weight > v.Weight {
			v.Weight = ppp.Weight
		} else if !ok {
			globmap[gp] = ppp
			p = append(p, ppp)
		}
	}
	return p
}

func parseMagic(m *magic) (p *parsedMagic, err error) {
	p = &parsedMagic{Priority: getPriority(m.Priority)}
	if p.Priority == invalidPriority {
		return nil, errors.New("invalid magic priority")
	}
	for _, mm := range m.Match {
		pp, err := parseMatch(mm)
		if err != nil {
			return nil, err
		}
		p.Match = append(p.Match, pp)
	}
	return
}

func parseMatch(m *match) (p *parsedMatch, err error) {
	if m.Offset == "" {
		return nil, errors.New("missing 'offset' attribute")
	}
	s := strings.Split(m.Offset, ":")
	if len(s) > 2 {
		return nil, errors.New("invalid offset")
	}

	p = new(parsedMatch)
	if p.RangeStart, err = parseOffset(s[0]); err != nil {
		return nil, err
	}

	if len(s) > 1 {
		if p.RangeLength, err = parseOffset(s[1]); err != nil {
			return nil, err
		}
		if p.RangeLength < p.RangeStart {
			return nil, errors.New("invalid offset")
		}
		p.RangeLength -= p.RangeStart
	}

	var byteSize int
	var byteOrder binary.ByteOrder = binary.BigEndian
	switch m.Type {
	case "":
		return nil, errors.New("missing 'type' attribute in <match>")
	case "byte":
		byteSize = 1
	case "host16", "big16":
		byteSize = 2
	case "little16":
		byteSize, byteOrder = 2, binary.LittleEndian
	case "host32", "big32":
		byteSize = 4
	case "little32":
		byteSize, byteOrder = 4, binary.LittleEndian
	case "string":
		p.Data, err = parseString(m.Value)
		if err != nil {
			return nil, err
		}
		if m.Mask != "" {
			p.Mask, err = parseStringMask(m.Mask)
			if err != nil {
				return nil, err
			}
			if len(p.Mask) != len(p.Data) {
				return nil, errors.New("string and mask lengths don't match")
			}
			for i, mask := range p.Mask {
				if mask == 0x00 {
					p.Data[i] = 0x00
				}
			}
		} else {
			p.Mask = nil
		}
	default:
		return nil, fmt.Errorf("unknown magic type '%v'", m.Type)
	}

	if byteSize != 0 {
		p.Data, err = parseInt(m.Value, byteSize, byteOrder)
		if err != nil {
			return nil, err
		}
		if m.Mask != "" {
			p.Mask, err = parseInt(m.Mask, byteSize, byteOrder)
			if err != nil {
				return nil, err
			}
		} else {
			p.Mask = nil
		}
	}

	for _, mm := range m.Match {
		pp, err := parseMatch(mm)
		if err != nil {
			return nil, err
		}
		p.Match = append(p.Match, pp)
	}
	return
}

func parseString(s string) ([]byte, error) {
	var runeTmp [utf8.UTFMax]byte
	buf := make([]byte, 0, 3*len(s)/2)
	for len(s) > 0 {
		c, multibyte, ss, err := strconv.UnquoteChar(s, 0)
		if err == strconv.ErrSyntax && len(s) > 1 && s[0] == '\\' {
			switch s[1] {
			case '0', '1', '2', '3', '4', '5', '6', '7':
				a, l := rune(s[1]-'0'), 2
				var b rune
				if len(s) > 2 && '0' <= s[2] && s[2] <= '7' {
					b = rune(s[2] - '0')
					l++
				} else {
					b = a
					a = 0
				}
				c, multibyte, ss, err = a*8+b, false, s[l:], nil
			case ' ', '"', '\'':
				c, multibyte, ss, err = rune(s[1]), false, s[2:], nil
			case 'x':
				if len(s) < 3 {
					break
				}
				v, ok := unhex(s[2])
				if !ok {
					break
				}
				c, multibyte, ss, err = v, false, s[3:], nil
			}
		}
		if err != nil {
			return nil, err
		}
		s = ss
		if c < utf8.RuneSelf || !multibyte {
			buf = append(buf, byte(c))
		} else {
			n := utf8.EncodeRune(runeTmp[:], c)
			buf = append(buf, runeTmp[:n]...)
		}
	}
	return buf, nil
}

func unhex(b byte) (v rune, ok bool) {
	c := rune(b)
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return
}

func parseStringMask(s string) ([]byte, error) {
	if s[0] != '0' || len(s) < 3 || (s[1] != 'x' && s[1] != 'X') {
		return nil, errors.New("invalid string mask")
	}
	if len(s)%2 == 1 {
		s = s[:1] + s[2:]
	} else {
		s = s[2:]
	}
	return hex.DecodeString(s)
}

func parseInt(s string, byteSize int, byteOrder binary.ByteOrder) ([]byte, error) {
	n, err := strconv.ParseUint(s, 0, byteSize*8)
	if err != nil {
		return nil, fmt.Errorf("invalid value: %v", err)
	}
	b := make([]byte, 8)
	byteOrder.PutUint64(b, n)
	if byteOrder == binary.BigEndian {
		return b[8-byteSize:], nil
	}
	return b[:byteSize], nil
}

func parseOffset(s string) (n int, err error) {
	if n, err = strconv.Atoi(s); err != nil {
		return 0, fmt.Errorf("invalid offset: %v", err)
	}
	if n < 0 || n > math.MaxInt32 {
		return 0, fmt.Errorf("offset out of range (%d should fit in 4 bytes)", n)
	}
	return
}
