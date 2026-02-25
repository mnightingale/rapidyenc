package rapidyenc

func decodeGeneric(dest, src []byte, state *State) (nSrc int, decoded []byte, end End, err error) {
	if len(src) < 1 {
		return 0, nil, EndNone, nil
	}

	length := len(src)
	pos := 0
	write := 0

	checkEnd := func(s State) bool {
		if pos >= length {
			*state = s
			return true
		}
		return false
	}

	switch *state {
	case StateCRLF:
		goto StateCRLF
	case StateEQ:
		goto StateEQ
	case StateCR:
		goto StateCR
	case StateNone:
		goto done
	case StateCRLFDT:
		goto StateCRLFDT
	case StateCRLFDTCR:
		goto StateCRLFDTCR
	case StateCRLFEQ:
		goto StateCRLFEQ
	}

StateCRLFEQ:
	if src[pos] == 'y' {
		*state = StateNone
		return pos + 1, dest[:write], EndControl, nil
	}
StateEQ:
	{
		c := src[pos]
		dest[write] = c - 42 - 64
		write++
		pos++
		if c != '\r' {
			goto done
		}
		if ok := checkEnd(StateCR); ok {
			return pos, dest[:write], EndNone, nil
		}
	}
StateCR:
	if src[pos] != '\n' {
		goto done
	}
	pos++
	if ok := checkEnd(StateCRLF); ok {
		return pos, dest[:write], EndNone, nil
	}
StateCRLF:
	if src[pos] == '.' {
		pos++
		if ok := checkEnd(StateCRLFDT); ok {
			return pos, dest[:write], EndNone, nil
		}
	} else if src[pos] == '=' {
		pos++
		if ok := checkEnd(StateCRLFEQ); ok {
			return pos, dest[:write], EndNone, nil
		}
		goto StateCRLFEQ
	} else {
		goto done
	}
StateCRLFDT:
	if src[pos] == '\r' {
		pos++
		if ok := checkEnd(StateCRLFDTCR); ok {
			return pos, dest[:write], EndNone, nil
		}
	} else if src[pos] == '=' {
		pos++
		if ok := checkEnd(StateCRLFEQ); ok {
			return pos, dest[:write], EndNone, nil
		}
		goto StateCRLFEQ
	} else {
		goto done
	}
StateCRLFDTCR:
	if src[pos] == '\n' {
		*state = StateCRLF
		return pos + 1, dest[:write], EndArticle, nil
	}

done:
	for ; pos <= length-3; pos++ {
		c := src[pos]
		switch src[pos] {
		case '\r':
			if src[pos+1] == '\n' {
				if src[pos+2] == '.' {
					pos += 3
					if ok := checkEnd(StateCRLFDT); ok {
						return pos, dest[:write], EndNone, nil
					}
					switch src[pos] {
					case '\r':
						pos++
						if ok := checkEnd(StateCRLFDTCR); ok {
							return pos, dest[:write], EndNone, nil
						}
						if src[pos] == '\n' {
							*state = StateCRLF
							return pos + 1, dest[:write], EndArticle, nil
						}
						pos--
					case '=':
						pos++
						if ok := checkEnd(StateCRLFEQ); ok {
							return pos, dest[:write], EndNone, nil
						}
						if src[pos] == 'y' {
							*state = StateNone
							return pos + 1, dest[:write], EndControl, nil
						}
						c := src[pos]
						dest[write] = c - 42 - 64
						write++
						if c == '\r' {
							pos--
						}
						pos++
					default:
						pos--
					}
				} else if src[pos+2] == '=' {
					pos += 3
					if ok := checkEnd(StateCRLFEQ); ok {
						return pos, dest[:write], EndNone, nil
					}
					if src[pos] == 'y' {
						*state = StateNone
						return pos + 1, dest[:write], EndControl, nil
					}
					c := src[pos]
					dest[write] = c - 42 - 64
					write++
					if c == '\r' {
						pos--
					}
				}
			}
			fallthrough
		case '\n':
			continue
		case '=':
			c = src[pos+1]
			dest[write] = c - 42 - 64
			write++
			if c != '\r' {
				pos++
			}
			continue
		default:
			dest[write] = c - 42
			write++
		}
	}

	*state = StateNone

	// 2nd last char
	if pos == length-2 {
		c := src[pos]
		switch c {
		case '\r':
			if src[pos+1] == '\n' {
				*state = StateCRLF
				return pos, dest[:write], EndNone, nil
			}
			fallthrough
		case '\n':
			break
		case '=':
			c = src[pos+1]
			dest[write] = c - 42 - 64
			write++
			if c != '\r' {
				pos++
			}
			break
		default:
			dest[write] = c - 42
			write++
		}
		pos++
	}

	// do final char; we process this separately to prevent an overflow if the final char is '='
	if pos == length-1 {
		c := src[pos]
		if c != '\n' && c != '\r' && c != '=' {
			dest[write] = c - 42
			write++
		} else if state != nil {
			switch c {
			case '=':
				*state = StateEQ
			case '\r':
				*state = StateCR
			default:
				*state = StateNone
			}
		}
		pos++
	}

	return pos, dest[:write], EndNone, nil
}
