# fish completion for acoo

function __acoo_perform_completion
    # Get the raw command line
    set -l raw (commandline -opc)
    set -l lastArg (commandline -ct)

    # Skip if no command
    if test -z "$raw"
        return 1
    end

    # Parse into parts
    set -l wordList (string split " " -- $raw)
    
    # Remove empty elements
    set -l args
    for w in $wordList
        if test -n "$w"
            set args $args $w
        end
    end
    
    # Need at least command name
    if test (count $args) -lt 1
        return 1
    end

    # Build the full command as a string for eval
    set -l cmdStr "ACOO_ACTIVE_HELP=0 $args[1] __complete"
    
    # Add remaining args
    if test (count $args) -ge 2
        for i in (seq 2 (count $args))
            set cmdStr "$cmdStr $args[$i]"
        end
    end
    
    # If cursor is after space with no partial, add empty arg to trigger completion
    if test -z "$lastArg" -a (count $args) -ge 2
        set cmdStr "$cmdStr ''"
    else if test -n "$lastArg"
        set cmdStr "$cmdStr '$lastArg'"
    end

    set -l results (eval $cmdStr 2> /dev/null)
    if test -z "$results"
        return 1
    end

    # Parse: last line is directive, rest are completions
    set -l comps
    if test (count $results) -gt 0
        set comps $results[1..-2]
    end

    # Output completions
    for comp in $comps
        set -l desc ""
        set -l parts (string split -m1 \t -- $comp)
        if test (count $parts) -eq 2
            set comp $parts[1]
            set desc $parts[2]
        end
        if test -n "$comp"
            printf "%s\n" "$comp"
        end
    end
end

# Register completion
complete -c acoo -f -a "(__acoo_perform_completion)"
