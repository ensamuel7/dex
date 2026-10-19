package resolve

import "strings"

var cLibraryNames = func() map[string]bool {
	names := map[string]bool{}
	for _, n := range strings.Fields(
		"DIR FILE a64l abort abs accept access accessx_np acct acos acosf acosh " +
			"acoshf acoshl acosl add_profil addr2ascii adjtime alarm aligned_alloc alloca " +
			"alphasort arc4random arc4random_addrandom arc4random_buf arc4random_stir " +
			"arc4random_uniform ascii2addr asctime asctime_r asin asinf asinh asinhf " +
			"asinhl asinl asprintf at_quick_exit atan atan2 atan2f atan2l atanf atanh " +
			"atanhf atanhl atanl atexit atexit_b atof atoi atol atoll atomic_bool " +
			"atomic_char atomic_char16_t atomic_char32_t atomic_compare_exchange_strong " +
			"atomic_compare_exchange_strong_explicit atomic_compare_exchange_weak " +
			"atomic_compare_exchange_weak_explicit atomic_exchange " +
			"atomic_exchange_explicit atomic_fetch_add atomic_fetch_add_explicit " +
			"atomic_fetch_and atomic_fetch_and_explicit atomic_fetch_or " +
			"atomic_fetch_or_explicit atomic_fetch_sub atomic_fetch_sub_explicit " +
			"atomic_fetch_xor atomic_fetch_xor_explicit atomic_flag atomic_flag_clear " +
			"atomic_flag_clear_explicit atomic_flag_test_and_set " +
			"atomic_flag_test_and_set_explicit atomic_init atomic_int atomic_int_fast16_t " +
			"atomic_int_fast32_t atomic_int_fast64_t atomic_int_fast8_t " +
			"atomic_int_least16_t atomic_int_least32_t atomic_int_least64_t " +
			"atomic_int_least8_t atomic_intmax_t atomic_intptr_t atomic_is_lock_free " +
			"atomic_llong atomic_load atomic_load_explicit atomic_long atomic_ptrdiff_t " +
			"atomic_schar atomic_short atomic_signal_fence atomic_size_t atomic_store " +
			"atomic_store_explicit atomic_thread_fence atomic_uchar atomic_uint " +
			"atomic_uint_fast16_t atomic_uint_fast32_t atomic_uint_fast64_t " +
			"atomic_uint_fast8_t atomic_uint_least16_t atomic_uint_least32_t " +
			"atomic_uint_least64_t atomic_uint_least8_t atomic_uintmax_t atomic_uintptr_t " +
			"atomic_ullong atomic_ulong atomic_ushort atomic_wchar_t bcmp bcopy bind " +
			"bindresvport bindresvport_sa blkcnt_t blksize_t bool brk bsd_signal bsearch " +
			"bsearch_b bzero caddr_t calloc cbrt cbrtf cbrtl cc_t ceil ceilf ceill " +
			"cfgetispeed cfgetospeed cfmakeraw cfsetispeed cfsetospeed cfsetspeed cgetcap " +
			"cgetclose cgetent cgetfirst cgetmatch cgetnext cgetnum cgetset cgetstr " +
			"cgetustr chdir chflags chmod chmodx_np chown chroot clearerr " +
			"clearerr_unlocked clock clock_getres clock_gettime clock_gettime_nsec_np " +
			"clock_settime clock_t clockid_t close closedir confstr connect connectx " +
			"copysign copysignf copysignl cos cosf cosh coshf coshl cosl creat crypt " +
			"ct_rune_t ctermid ctermid_r ctime ctime_r d_fileno daddr_t daemon daylight " +
			"dev_t devname devname_r difftime digittoint dirfd disconnectx div div_t " +
			"double_t dprintf drand48 dup dup2 ecvt encrypt endhostent endnetent " +
			"endnetgrent endprotoent endrpcent endservent endusershell epoll_create " +
			"epoll_create1 epoll_ctl epoll_pwait epoll_wait erand48 erf erfc erfcf erfcl " +
			"erff erfl errno errno_t exchangedata execl execle execlp execv execvP execve " +
			"execvp exit exp exp2 exp2f exp2l expf expl expm1 expm1f expm1l fabs fabsf " +
			"fabsl faccessat false fattributiontag_t fchdir fchecklv_t fchflags fchmod " +
			"fchmodat fchmodx_np fchown fchownat fclose fcntl fcvt fd_mask fd_set fdim " +
			"fdimf fdiml fdopen fdopendir feof feof_unlocked ferror ferror_unlocked " +
			"fflagstostr fflush ffs ffsctl ffsl ffsll fgetattrlist fgetc fgetln fgetpos " +
			"fgets fgetsigsinfo_t fileno fileno_unlocked filesec_dup filesec_free " +
			"filesec_get_property filesec_init filesec_property_t filesec_query_property " +
			"filesec_set_property filesec_t filesec_unset_property fixpt_t float_t flock " +
			"flockfile floor floorf floorl fls flsl flsll fma fmaf fmal fmax fmaxf fmaxl " +
			"fmemopen fmin fminf fminl fmod fmodf fmodl fmtcheck fopen fork fpathconf " +
			"fpclassify fpos_t fprintf fpunchhole_t fpurge fputc fputs fread freadlink " +
			"free freeaddrinfo freehostent freopen frexp frexpf frexpl fropen fsblkcnt_t " +
			"fscanf fsctl fseek fseeko fsetattrlist fsetpos fsfilcnt_t fsignatures_t " +
			"fspecread_t fstat fstatat fstatx_np fstore_t fsupplement_t fsync " +
			"fsync_volume_np ftell ftello ftrimactivefile_t ftruncate ftrylockfile " +
			"funlockfile funopen futimens futimes fwopen fwrite gai_strerror gcvt " +
			"getaddrinfo getattrlist getattrlistat getattrlistbulk getbsize getc " +
			"getc_unlocked getchar getchar_unlocked getcwd getdate getdate_err getdelim " +
			"getdirentries getdirentriesattr getdomainname getdtablesize getegid " +
			"getentropy getenv geteuid getgid getgrouplist getgroups gethostbyaddr " +
			"gethostbyname gethostbyname2 gethostent gethostid gethostname gethostuuid " +
			"getiopolicy_np getipnodebyaddr getipnodebyname getipv4sourcefilter getitimer " +
			"getline getloadavg getlogin getlogin_r getmode getnameinfo getnetbyaddr " +
			"getnetbyname getnetent getnetgrent getopt getpagesize getpass getpeereid " +
			"getpeername getpgid getpgrp getpid getppid getpriority getprogname " +
			"getprotobyname getprotobynumber getprotoent getrandom getrlimit getrpcbyname " +
			"getrpcbynumber getrpcent getrusage gets getservbyname getservbyport " +
			"getservent getsgroups_np getsid getsockname getsockopt getsourcefilter " +
			"getsubopt gettimeofday getuid getusershell getw getwd getwgroups_np gid_t " +
			"gmtime gmtime_r grantpt h_addr h_errno heapsort heapsort_b herror howmany " +
			"hstrerror htonl htonll htons hypot hypotf hypotl id_t idtype_t ilogb ilogbf " +
			"ilogbl in6_addr_t in6addr_any in6addr_linklocal_allnodes " +
			"in6addr_linklocal_allrouters in6addr_linklocal_allv2routers in6addr_loopback " +
			"in6addr_nodelocal_allnodes in_addr_t in_port_t index inet6_opt_append " +
			"inet6_opt_find inet6_opt_finish inet6_opt_get_val inet6_opt_init " +
			"inet6_opt_next inet6_opt_set_val inet6_option_alloc inet6_option_append " +
			"inet6_option_find inet6_option_init inet6_option_next inet6_option_space " +
			"inet6_rth_add inet6_rth_getaddr inet6_rth_init inet6_rth_reverse " +
			"inet6_rth_segments inet6_rth_space inet6_rthdr_add inet6_rthdr_getaddr " +
			"inet6_rthdr_getflags inet6_rthdr_init inet6_rthdr_lasthop " +
			"inet6_rthdr_segments inet6_rthdr_space inet_addr inet_aton inet_lnaof " +
			"inet_makeaddr inet_net_ntop inet_net_pton inet_neta inet_netof inet_network " +
			"inet_nsap_addr inet_nsap_ntoa inet_ntoa inet_ntop inet_pton initgroups " +
			"initstate innetgr ino64_t ino_t int16_t int32_t int64_t int8_t int_fast16_t " +
			"int_fast32_t int_fast64_t int_fast8_t int_least16_t int_least32_t " +
			"int_least64_t int_least8_t intmax_t intptr_t iruserok iruserok_sa isalnum " +
			"isalpha isascii isatty isblank iscntrl isdigit isfinite isgraph isgreater " +
			"isgreaterequal ishexnumber isideogram isinf isless islessequal islessgreater " +
			"islower isnan isnormal isnumber isphonogram isprint ispunct isrune issetugid " +
			"isspace isspecial isunordered isupper isxdigit j0 j1 jmp_buf jn jrand48 " +
			"kevent kevent64 key_t kill kill_dependency killpg kqueue l64a labs lchflags " +
			"lchmod lchown lcong48 ldexp ldexpf ldexpl ldiv ldiv_t lgamma lgammaf lgammal " +
			"link linkat listen llabs lldiv lldiv_t llrint llrintf llrintl llround " +
			"llroundf llroundl localtime localtime_r lockf log log10 log10f log10l log1p " +
			"log1pf log1pl log2 log2f log2l logb logbf logbl logf logl longjmp " +
			"longjmperror lrand48 lrint lrintf lrintl lround lroundf lroundl lseek lstat " +
			"lstatx_np lutimes mach_port_t major makedev malloc malloc_type_aligned_alloc " +
			"malloc_type_calloc malloc_type_free malloc_type_id_t malloc_type_malloc " +
			"malloc_type_posix_memalign malloc_type_realloc malloc_type_valloc " +
			"malloc_type_zone_calloc malloc_type_zone_free malloc_type_zone_malloc " +
			"malloc_type_zone_memalign malloc_type_zone_realloc malloc_type_zone_valloc " +
			"malloc_zone_t math_errhandling max_align_t mblen mbstowcs mbtowc mcontext_t " +
			"memccpy memchr memcmp memcpy memmem memmove memory_order mempcpy memrchr " +
			"memset memset_pattern16 memset_pattern4 memset_pattern8 memset_s mergesort " +
			"mergesort_b minor mkdir mkdirat mkdirx_np mkdtemp mkdtempat_np mkfifo " +
			"mkfifoat mkfifox_np mknod mknodat mkostemp mkostemps mkostempsat_np " +
			"mkpath_np mkpathat_np mkstemp mkstemp_dprotected_np mkstemps mkstempsat_np " +
			"mktemp mktime mode_t modf modff modfl mrand48 nan nanf nanl nanosleep " +
			"nearbyint nearbyintf nearbyintl nextafter nextafterf nextafterl nexttoward " +
			"nexttowardf nexttowardl nfds_t nfssvc nice nlink_t nrand48 ntohl ntohll " +
			"ntohs off_t offsetof open open_dprotected_np open_memstream openat " +
			"openat_authenticated_np openat_dprotected_np opendir openx_np optarg opterr " +
			"optind optopt pathconf pause pclose perror pfctlinput pid_t pipe poll popen " +
			"posix2time posix_memalign posix_openpt pow powf powl pread preadv printf " +
			"profil pselect pseudo_AF_HDRCMPLT pseudo_AF_KEY pseudo_AF_PIP pseudo_AF_RTIP " +
			"pseudo_AF_XTP psignal psort psort_b psort_r pthread_atfork " +
			"pthread_attr_destroy pthread_attr_get_qos_class_np " +
			"pthread_attr_getdetachstate pthread_attr_getguardsize " +
			"pthread_attr_getinheritsched pthread_attr_getschedparam " +
			"pthread_attr_getschedpolicy pthread_attr_getscope pthread_attr_getstack " +
			"pthread_attr_getstackaddr pthread_attr_getstacksize pthread_attr_init " +
			"pthread_attr_set_qos_class_np pthread_attr_setdetachstate " +
			"pthread_attr_setguardsize pthread_attr_setinheritsched " +
			"pthread_attr_setschedparam pthread_attr_setschedpolicy pthread_attr_setscope " +
			"pthread_attr_setstack pthread_attr_setstackaddr pthread_attr_setstacksize " +
			"pthread_attr_t pthread_cancel pthread_cleanup_pop pthread_cleanup_push " +
			"pthread_cond_broadcast pthread_cond_destroy pthread_cond_init " +
			"pthread_cond_signal pthread_cond_signal_thread_np pthread_cond_t " +
			"pthread_cond_timedwait pthread_cond_timedwait_relative_np pthread_cond_wait " +
			"pthread_condattr_destroy pthread_condattr_getpshared pthread_condattr_init " +
			"pthread_condattr_setpshared pthread_condattr_t pthread_cpu_number_np " +
			"pthread_create pthread_create_suspended_np pthread_detach pthread_equal " +
			"pthread_exit pthread_from_mach_thread_np pthread_get_qos_class_np " +
			"pthread_get_stackaddr_np pthread_get_stacksize_np pthread_getconcurrency " +
			"pthread_getname_np pthread_getschedparam pthread_getspecific " +
			"pthread_getugid_np pthread_is_threaded_np pthread_jit_write_callback_t " +
			"pthread_jit_write_freeze_callbacks_np pthread_jit_write_protect_np " +
			"pthread_jit_write_protect_supported_np pthread_jit_write_with_callback_np " +
			"pthread_join pthread_key_create pthread_key_delete pthread_key_t " +
			"pthread_kill pthread_mach_thread_np pthread_main_np pthread_mutex_destroy " +
			"pthread_mutex_getprioceiling pthread_mutex_init pthread_mutex_lock " +
			"pthread_mutex_setprioceiling pthread_mutex_t pthread_mutex_trylock " +
			"pthread_mutex_unlock pthread_mutexattr_destroy " +
			"pthread_mutexattr_getpolicy_np pthread_mutexattr_getprioceiling " +
			"pthread_mutexattr_getprotocol pthread_mutexattr_getpshared " +
			"pthread_mutexattr_gettype pthread_mutexattr_init " +
			"pthread_mutexattr_setpolicy_np pthread_mutexattr_setprioceiling " +
			"pthread_mutexattr_setprotocol pthread_mutexattr_setpshared " +
			"pthread_mutexattr_settype pthread_mutexattr_t pthread_once pthread_once_t " +
			"pthread_override_qos_class_end_np pthread_override_qos_class_start_np " +
			"pthread_override_t pthread_rwlock_destroy pthread_rwlock_init " +
			"pthread_rwlock_rdlock pthread_rwlock_t pthread_rwlock_tryrdlock " +
			"pthread_rwlock_trywrlock pthread_rwlock_unlock pthread_rwlock_wrlock " +
			"pthread_rwlockattr_destroy pthread_rwlockattr_getpshared " +
			"pthread_rwlockattr_init pthread_rwlockattr_setpshared pthread_rwlockattr_t " +
			"pthread_self pthread_set_qos_class_self_np pthread_setcancelstate " +
			"pthread_setcanceltype pthread_setconcurrency pthread_setname_np " +
			"pthread_setschedparam pthread_setspecific pthread_setugid_np pthread_sigmask " +
			"pthread_t pthread_testcancel pthread_threadid_np pthread_yield_np ptrdiff_t " +
			"ptsname ptsname_r putc putc_unlocked putchar putchar_unlocked putenv puts " +
			"putw pwrite pwritev qaddr_t qos_class_main qos_class_self qos_class_t qsort " +
			"qsort_b qsort_r quad_t quick_exit radixsort raise rand rand_r random " +
			"rawmemchr rcmd rcmd_af read readdir readdir_r readlink readlinkat readv " +
			"realloc reallocf realpath reboot recv recvfrom recvmsg register_t remainder " +
			"remainderf remainderl remove remquo remquof remquol rename renameat " +
			"renameatx_np renamex_np revoke rewind rewinddir rindex rint rintf rintl " +
			"rlim_t rmdir round roundf roundl rpmatch rresvport rresvport_af rsize_t " +
			"ru_first ru_last rune_t rusage_info_current rusage_info_t ruserok s6_addr " +
			"sa_endpoints_t sa_family_t sa_handler sa_sigaction sae_associd_t " +
			"sae_connid_t sbrk scalb scalbln scalblnf scalblnl scalbn scalbnf scalbnl " +
			"scandir scandir_b scanf sched_get_priority_max sched_get_priority_min " +
			"sched_yield searchfs secure_getenv seed48 seekdir segsz_t select send " +
			"sendfile sendmsg sendto setattrlist setattrlistat setbuf setbuffer " +
			"setdomainname setegid setenv seteuid setgid setgroups sethostent sethostid " +
			"sethostname setiopolicy_np setipv4sourcefilter setitimer setjmp setkey " +
			"setlinebuf setlogin setmode setnetent setnetgrent setpgid setpgrp " +
			"setpriority setprogname setprotoent setregid setreuid setrgid setrlimit " +
			"setrpcent setruid setservent setsgroups_np setsid setsockopt setsourcefilter " +
			"setstate settimeofday setuid setusershell setvbuf setwgroups_np shutdown " +
			"sig_atomic_t sig_t sigaction sigaddset sigaltstack sigblock sigdelset " +
			"sigemptyset sigfillset sighold sigignore siginfo_t siginterrupt sigismember " +
			"sigjmp_buf siglongjmp sigmask signal signbit signgam sigpause sigpending " +
			"sigprocmask sigrelse sigset sigset_t sigsetjmp sigsetmask sigsuspend sigvec " +
			"sigwait sin sinf sinh sinhf sinhl sinl size_t sleep snprintf sockatmark " +
			"socket socketpair socklen_t speed_t sprintf sqrt sqrtf sqrtl sradixsort " +
			"srand srand48 sranddev srandom srandomdev sscanf ssize_t st_atime " +
			"st_birthtime st_ctime st_mtime stack_t stat statx_np stderr stdin stdout " +
			"stpcpy stpncpy strcasecmp strcasestr strcat strchr strchrnul strcmp strcoll " +
			"strcpy strcspn strdup strdupa strerror strerror_r strftime strlcat strlcpy " +
			"strlen strmode strncasecmp strncat strncmp strncpy strndup strndupa strnlen " +
			"strnstr strpbrk strptime strrchr strsep strsignal strsignal_r strspn strstr " +
			"strtod strtof strtofflags strtok strtok_r strtol strtold strtoll strtonum " +
			"strtoq strtoul strtoull strtouq strxfrm suboptarg suseconds_t sv_onstack " +
			"swab swapon swblk_t symlink symlinkat sync sync_volume_np sys_errlist " +
			"sys_nerr sys_siglist sys_signame syscall syscall_arg_t sysconf system tan " +
			"tanf tanh tanhf tanhl tanl tcdrain tcflag_t tcflow tcflush tcgetattr " +
			"tcgetpgrp tcgetsid tcp6_seq tcp6hdr tcp_cc tcp_seq tcsendbreak tcsetattr " +
			"tcsetpgrp telldir tempnam tgamma tgammaf tgammal time time2posix time_t " +
			"timegm timelocal timeradd timerclear timercmp timerisset timersub " +
			"timespec_get timevalcmp timezone timingsafe_bcmp tmpfile tmpnam toascii " +
			"tolower toupper true trunc truncate truncf truncl ttyname ttyname_r ttyslot " +
			"tzname tzset tzsetwall u_char u_int u_int16_t u_int32_t u_int64_t u_int8_t " +
			"u_long u_quad_t u_short ualarm ucontext_t uid_t uint uint16_t uint32_t " +
			"uint64_t uint8_t uint_fast16_t uint_fast32_t uint_fast64_t uint_fast8_t " +
			"uint_least16_t uint_least32_t uint_least64_t uint_least8_t uintmax_t " +
			"uintptr_t umask umaskx_np undelete ungetc unlink unlinkat unlockpt unsetenv " +
			"unwhiteout useconds_t user_addr_t user_long_t user_off_t user_size_t " +
			"user_ssize_t user_time_t user_ulong_t ushort usleep utimensat utimes uuid_t " +
			"va_list valloc vasprintf vdprintf vfork vfprintf vfscanf vprintf vscanf " +
			"vsnprintf vsprintf vsscanf w_coredump w_retcode w_stopsig w_stopval " +
			"w_termsig wait wait3 wait4 waitid waitpid wchar_t wcstombs wctomb wint_t " +
			"write writev y0 y1 yn ",
	) {
		names[n] = true
	}
	return names
}()
